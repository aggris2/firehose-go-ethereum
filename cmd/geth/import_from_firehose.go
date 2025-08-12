package main

import (
	"context"
	"fmt"
	"github.com/avast/retry-go"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/streamingfast/firehose-core/firehose/client"
	pbeth "github.com/streamingfast/firehose-ethereum/types/pb/sf/ethereum/type/v2"
	pbfirehose "github.com/streamingfast/pbgo/sf/firehose/v2"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"
	"io"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"
)

func importFromFirehose(ctx *cli.Context) error {
	if ctx.Args().Len() < 3 {
		return fmt.Errorf("usage: import-from-firehose <firehose-endpoint> <chainID> <rpc>")
	}

	apiToken := os.Getenv("FIREHOSE_API_TOKEN")
	endpoint := ctx.Args().Get(0)
	chainIDStr := ctx.Args().Get(1)
	externalRpc := ctx.Args().Get(2)
	batchSize := ctx.Int("batch-size")
	endBlock := ctx.Uint64("end-block")
	workerCount := ctx.Int("worker-count")
	bufferSize := ctx.Int("firehose-buffer-size")

	chainID := new(big.Int)
	if _, ok := chainID.SetString(chainIDStr, 10); !ok {
		return fmt.Errorf("invalid chainID: %s", chainIDStr)
	}

	// Open Geth stack and chain
	stack, cfg := makeConfigNode(ctx)
	defer stack.Close()
	utils.SetupMetrics(&cfg.Metrics)
	chain, db := utils.MakeChain(ctx, stack, false)
	defer db.Close()
	defer chain.Stop()

	var startBlock int
	if ctx.IsSet("start-block") {
		startBlock = ctx.Int("start-block")
		if startBlock < 0 {
			return fmt.Errorf("startBlock must be non-negative")
		}
		fmt.Printf("Starting from user-specified block: %d\n", startBlock)
	} else {
		// Resume from the local chain head + 1
		head := chain.CurrentBlock()
		if head != nil {
			startBlock = int(head.Number.Uint64() + 1)
			fmt.Printf("No start block specified. Resuming from last imported block: %d\n", startBlock)
		} else {
			startBlock = 0 // start from genesis
			fmt.Println("No start block specified and no local chain found. Starting from genesis (block 0).")
		}
	}

	var totalBlocks int
	currentBlock := startBlock
	err := processFirehoseBlocksWithReconnect(endpoint, apiToken, &currentBlock, endBlock, batchSize, workerCount, bufferSize, chainID, externalRpc, func(blocks []*types.Block, batchNum int) error {
		if len(blocks) == 0 {
			return nil
		}
		firstNum := blocks[0].NumberU64()
		lastNum := blocks[len(blocks)-1].NumberU64()
		if _, err := chain.InsertChain(blocks); err != nil {
			fmt.Printf("failed to import batch %d (blocks %d-%d): %v\n", batchNum, firstNum, lastNum, err)
			return err
		}
		fmt.Printf("Imported batch %d of %d blocks (blocks %d-%d)\n", batchNum, len(blocks), firstNum, lastNum)
		totalBlocks += len(blocks)
		currentBlock = int(lastNum + 1)
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("Import completed successfully. Total blocks imported: %d\n", totalBlocks)
	return nil
}

func processFirehoseBlocksWithReconnect(
	endpoint string,
	apiToken string,
	startBlock *int,
	endBlock uint64,
	batchSize int,
	workerCount int,
	bufferSize int,
	chainID *big.Int,
	externalRpc string,
	handler func(blocks []*types.Block, batchNum int) error,
) error {
	maxRetries := 100
	attempt := 0

	for attempt < maxRetries {
		prev := *startBlock

		err := retry.Do(
			func() error {
				return processFirehoseBlocks(endpoint, apiToken, startBlock, endBlock, batchSize, workerCount, bufferSize, chainID, externalRpc, handler)
			},
			retry.Attempts(1),
			retry.DelayType(retry.BackOffDelay),
			retry.Delay(time.Second*2),
			retry.MaxDelay(time.Minute*5),
		)

		if err == nil {
			return nil // success
		}

		if *startBlock > prev {
			fmt.Printf("Progress detected (%d → %d), resetting attempts\n", prev, *startBlock)
			attempt = 0
			continue
		}

		if isRetryableError(err) {
			fmt.Printf("Retryable error: %v\n", err)
			attempt++
			continue
		}

		// Non-retryable error
		return err
	}

	return fmt.Errorf("exceeded max retries (%d)", maxRetries)
}

func processFirehoseBlocks(
	endpoint string,
	apiToken string,
	startBlock *int,
	endBlock uint64,
	batchSize int,
	workerCount int,
	bufferSize int,
	chainID *big.Int,
	externalRpc string,
	handler func(blocks []*types.Block, batchNum int) error,
) error {
	client, closeFunc, grpcOpts, err := client.NewFirehoseClient(endpoint, apiToken, "", false, false)
	if err != nil {
		return fmt.Errorf("failed to create Firehose client: %w", err)
	}
	defer closeFunc()
	grpcOpts = append(grpcOpts, grpc.UseCompressor(gzip.Name))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	stream, err := client.Blocks(ctx, &pbfirehose.Request{
		StartBlockNum: int64(*startBlock),
		StopBlockNum:  endBlock,
	}, grpcOpts...)
	if err != nil {
		return fmt.Errorf("failed to start block stream: %w", err)
	}

	type seqResponse struct {
		seq  uint64
		resp *pbfirehose.Response
	}
	type seqBlock struct {
		seq   uint64
		block *types.Block
	}

	rawCh := make(chan seqResponse, bufferSize)
	blockCh := make(chan seqBlock, bufferSize)
	errCh := make(chan error, 1)
	doneCh := make(chan struct{})

	// Stage 1: Stream reader goroutine with sequence numbers
	go func() {
		defer close(rawCh)
		var seq uint64
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				errCh <- fmt.Errorf("error receiving from stream: %w", err)
				return
			}
			rawCh <- seqResponse{seq: seq, resp: resp}
			seq++
		}
	}()

	// Stage 2: Converter pool
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for sr := range rawCh {
				// Now it's this block's turn
				ethBlock := &pbeth.Block{}
				if err := sr.resp.Block.UnmarshalTo(ethBlock); err != nil {
					fmt.Printf("failed to unmarshal block (seq: %d): %v\n", sr.seq, err)
					continue
				}
				block, err := convertFirehoseBlockToGethBlock(ethBlock, chainID, externalRpc)
				if err != nil {
					fmt.Printf("failed to convert block %d: %v\n", ethBlock.Number, err)
					continue
				}
				blockCh <- seqBlock{seq: sr.seq, block: block}
			}
		}()
	}

	// Close blockCh when all workers are done
	go func() {
		wg.Wait()
		close(blockCh)
	}()

	// Stage 3: Batching and processing in order
	go func() {
		var (
			blocks   []*types.Block
			batchNum int
			nextSeq  uint64 = 0
			buffer          = make(map[uint64]*types.Block)
		)
		for sb := range blockCh {
			buffer[sb.seq] = sb.block
			// Drain in-order blocks from buffer
			for {
				block, ok := buffer[nextSeq]
				if !ok {
					break
				}
				blocks = append(blocks, block)
				delete(buffer, nextSeq)
				nextSeq++
				if len(blocks) >= batchSize {
					if err := handler(blocks, batchNum); err != nil {
						errCh <- err
						return
					}
					*startBlock = int(blocks[len(blocks)-1].NumberU64() + 1)
					batchNum++
					blocks = blocks[:0]
				}
			}
		}
		// Process any remaining blocks
		if len(blocks) > 0 {
			if err := handler(blocks, batchNum); err != nil {
				errCh <- err
				return
			}
			*startBlock = int(blocks[len(blocks)-1].NumberU64() + 1)
		}
		close(doneCh)
	}()

	// Wait for completion or error
	select {
	case err := <-errCh:
		return err
	case <-doneCh:
		return nil
	}
}

func isRetryableError(err error) bool {
	errStr := err.Error()
	// Check for common retryable errors
	retryableErrors := []string{
		"unexpected EOF",
		"connection reset by peer",
		"broken pipe",
		"context deadline exceeded",
		"transport is closing",
		"code = Unavailable",
		"code = Internal",
		"code = DeadlineExceeded",
		"rpc error",
	}

	for _, retryableErr := range retryableErrors {
		if strings.Contains(errStr, retryableErr) {
			return true
		}
	}
	return false
}
