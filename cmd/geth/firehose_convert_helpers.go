package main

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
	pbeth "github.com/streamingfast/firehose-ethereum/types/pb/sf/ethereum/type/v2"
	"math/big"
	"os"
	"path/filepath"
)

// Helper to convert Firehose logs to geth logs
func convertFirehoseLogsToGethLogs(pbLogs []*pbeth.Log) []*types.Log {
	logs := make([]*types.Log, 0, len(pbLogs))
	for _, pbLog := range pbLogs {
		if pbLog == nil {
			continue
		}
		log := &types.Log{
			Address:     common.BytesToAddress(pbLog.Address),
			Topics:      convertFirehoseTopicsToGethTopics(pbLog.Topics),
			Data:        pbLog.Data,
			BlockNumber: uint64(pbLog.BlockIndex),
			Index:       uint(pbLog.Index),
			// Ordinal:     pbLog.Ordinal,
			// TxHash:      common.Hash(pbLog.TransactionHash),
			// TxIndex:     uint(pbLog.TransactionIndex),
			// Removed:     pbLog.Removed,
		}
		logs = append(logs, log)
	}
	return logs
}

// Helper to convert Firehose topics to geth topics
func convertFirehoseTopicsToGethTopics(pbTopics [][]byte) []common.Hash {
	topics := make([]common.Hash, len(pbTopics))
	for i, t := range pbTopics {
		topics[i] = common.BytesToHash(t)
	}
	return topics
}

// Helper to convert Firehose AccessList to geth AccessList
func convertFirehoseAccessList(pbList []*pbeth.AccessTuple) types.AccessList {
	if len(pbList) == 0 {
		return nil
	}
	alist := make(types.AccessList, len(pbList))
	for i, tuple := range pbList {
		alist[i] = types.AccessTuple{
			Address:     common.BytesToAddress(tuple.Address),
			StorageKeys: convertFirehoseStorageKeys(tuple.StorageKeys),
		}
	}
	return alist
}

func convertFirehoseSetCodeAuthorizations(pbAuths []*pbeth.SetCodeAuthorization) []types.SetCodeAuthorization {
	if len(pbAuths) == 0 {
		return nil
	}
	auths := make([]types.SetCodeAuthorization, len(pbAuths))
	for i, pb := range pbAuths {
		auths[i] = types.SetCodeAuthorization{
			ChainID: *uint256.MustFromBig(new(big.Int).SetBytes(pb.ChainId)),
			Address: common.BytesToAddress(pb.Address),
			Nonce:   pb.Nonce,
			V:       uint8(pb.V),
			R:       *uint256.MustFromBig(new(big.Int).SetBytes(pb.R)),
			S:       *uint256.MustFromBig(new(big.Int).SetBytes(pb.S)),
		}
	}
	return auths
}

func convertFirehoseStorageKeys(pbKeys [][]byte) []common.Hash {
	if len(pbKeys) == 0 {
		return nil
	}
	keys := make([]common.Hash, len(pbKeys))
	for i, k := range pbKeys {
		keys[i] = common.BytesToHash(k)
	}
	return keys
}

// Helper to convert Firehose BlobHashes to geth []common.Hash
func convertFirehoseBlobHashes(pbHashes [][]byte) []common.Hash {
	if len(pbHashes) == 0 {
		return nil
	}
	hashes := make([]common.Hash, len(pbHashes))
	for i, h := range pbHashes {
		hashes[i] = common.BytesToHash(h)
	}
	return hashes
}

// Helper to convert big.Int to uint256.Int
func bigIntToUint256(b *big.Int) *uint256.Int {
	if b == nil {
		return uint256.NewInt(0)
	}
	return uint256.MustFromBig(b)
}

// Extracts chainID from the first non-nil, non-discarded SetCodeAuthorization
func extractChainIDFromSetCodeAuth(pbAuths []*pbeth.SetCodeAuthorization) *uint256.Int {
	for _, auth := range pbAuths {
		if auth == nil || auth.Discarded {
			continue
		}
		if len(auth.ChainId) > 0 {
			return uint256.MustFromBig(new(big.Int).SetBytes(auth.ChainId))
		}
	}
	return nil
}

// writeBatch writes a batch of blocks to an RLP file
func writeBatch(blocks []*types.Block, outputPrefix string, batchNum int) error {
	dir := "rlp-data"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	var filename string
	if batchNum == 0 {
		filename = filepath.Join(dir, fmt.Sprintf("%s.rlp", outputPrefix))
	} else {
		filename = filepath.Join(dir, fmt.Sprintf("%s.rlp.%d", outputPrefix, batchNum))
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}
	defer file.Close()

	// Write each block as RLP encoded data
	for _, block := range blocks {
		if err := rlp.Encode(file, block); err != nil {
			return fmt.Errorf("failed to encode block %d: %w", block.NumberU64(), err)
		}
	}

	return nil
}
