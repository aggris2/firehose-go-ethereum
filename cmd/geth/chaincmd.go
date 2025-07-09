// Copyright 2015 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pbeth "github.com/streamingfast/firehose-ethereum/types/pb/sf/ethereum/type/v2"
	pbfirehose "github.com/streamingfast/pbgo/sf/firehose/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/history"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/debug"
	"github.com/ethereum/go-ethereum/internal/era"
	"github.com/ethereum/go-ethereum/internal/era/eradl"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/streamingfast/firehose-core/firehose/client"
	"github.com/urfave/cli/v2"
)

var (
	initCommand = &cli.Command{
		Action:    initGenesis,
		Name:      "init",
		Usage:     "Bootstrap and initialize a new genesis block",
		ArgsUsage: "<genesisPath>",
		Flags: slices.Concat([]cli.Flag{
			utils.CachePreimagesFlag,
			utils.OverridePrague,
			utils.OverrideVerkle,
		}, utils.DatabaseFlags),
		Description: `
The init command initializes a new genesis block and definition for the network.
This is a destructive action and changes the network in which you will be
participating.

It expects the genesis file as argument.`,
	}
	dumpGenesisCommand = &cli.Command{
		Action:    dumpGenesis,
		Name:      "dumpgenesis",
		Usage:     "Dumps genesis block JSON configuration to stdout",
		ArgsUsage: "",
		Flags:     slices.Concat([]cli.Flag{utils.DataDirFlag}, utils.NetworkFlags),
		Description: `
The dumpgenesis command prints the genesis configuration of the network preset
if one is set.  Otherwise it prints the genesis from the datadir.`,
	}
	importCommand = &cli.Command{
		Action:    importChain,
		Name:      "import",
		Usage:     "Import a blockchain file",
		ArgsUsage: "<filename> (<filename 2> ... <filename N>) ",
		Flags: slices.Concat([]cli.Flag{
			utils.GCModeFlag,
			utils.SnapshotFlag,
			utils.CacheFlag,
			utils.CacheDatabaseFlag,
			utils.CacheTrieFlag,
			utils.CacheGCFlag,
			utils.CacheSnapshotFlag,
			utils.CacheNoPrefetchFlag,
			utils.CachePreimagesFlag,
			utils.NoCompactionFlag,
			utils.MetricsEnabledFlag,
			utils.MetricsEnabledExpensiveFlag,
			utils.MetricsHTTPFlag,
			utils.MetricsPortFlag,
			utils.MetricsEnableInfluxDBFlag,
			utils.MetricsEnableInfluxDBV2Flag,
			utils.MetricsInfluxDBEndpointFlag,
			utils.MetricsInfluxDBDatabaseFlag,
			utils.MetricsInfluxDBUsernameFlag,
			utils.MetricsInfluxDBPasswordFlag,
			utils.MetricsInfluxDBTagsFlag,
			utils.MetricsInfluxDBTokenFlag,
			utils.MetricsInfluxDBBucketFlag,
			utils.MetricsInfluxDBOrganizationFlag,
			utils.TxLookupLimitFlag,
			utils.VMTraceFlag,
			utils.VMTraceJsonConfigFlag,
			utils.TransactionHistoryFlag,
			utils.LogHistoryFlag,
			utils.LogNoHistoryFlag,
			utils.LogExportCheckpointsFlag,
			utils.StateHistoryFlag,
		}, utils.DatabaseFlags, debug.Flags),
		Before: func(ctx *cli.Context) error {
			flags.MigrateGlobalFlags(ctx)
			return debug.Setup(ctx)
		},
		Description: `
The import command allows the import of blocks from an RLP-encoded format. This format can be a single file
containing multiple RLP-encoded blocks, or multiple files can be given.

If only one file is used, an import error will result in the entire import process failing. If
multiple files are processed, the import process will continue even if an individual RLP file fails
to import successfully.`,
	}
	exportCommand = &cli.Command{
		Action:    exportChain,
		Name:      "export",
		Usage:     "Export blockchain into file",
		ArgsUsage: "<filename> [<blockNumFirst> <blockNumLast>]",
		Flags:     slices.Concat([]cli.Flag{utils.CacheFlag}, utils.DatabaseFlags),
		Description: `
Requires a first argument of the file to write to.
Optional second and third arguments control the first and
last block to write. In this mode, the file will be appended
if already existing. If the file ends with .gz, the output will
be gzipped.`,
	}
	importHistoryCommand = &cli.Command{
		Action:    importHistory,
		Name:      "import-history",
		Usage:     "Import an Era archive",
		ArgsUsage: "<dir>",
		Flags:     slices.Concat([]cli.Flag{utils.TxLookupLimitFlag, utils.TransactionHistoryFlag}, utils.DatabaseFlags, utils.NetworkFlags),
		Description: `
The import-history command will import blocks and their corresponding receipts
from Era archives.
`,
	}
	exportHistoryCommand = &cli.Command{
		Action:    exportHistory,
		Name:      "export-history",
		Usage:     "Export blockchain history to Era archives",
		ArgsUsage: "<dir> <first> <last>",
		Flags:     utils.DatabaseFlags,
		Description: `
The export-history command will export blocks and their corresponding receipts
into Era archives. Eras are typically packaged in steps of 8192 blocks.
`,
	}
	importPreimagesCommand = &cli.Command{
		Action:    importPreimages,
		Name:      "import-preimages",
		Usage:     "Import the preimage database from an RLP stream",
		ArgsUsage: "<datafile>",
		Flags:     slices.Concat([]cli.Flag{utils.CacheFlag}, utils.DatabaseFlags),
		Description: `
The import-preimages command imports hash preimages from an RLP encoded stream.
It's deprecated, please use "geth db import" instead.
`,
	}

	dumpCommand = &cli.Command{
		Action:    dump,
		Name:      "dump",
		Usage:     "Dump a specific block from storage",
		ArgsUsage: "[? <blockHash> | <blockNum>]",
		Flags: slices.Concat([]cli.Flag{
			utils.CacheFlag,
			utils.IterativeOutputFlag,
			utils.ExcludeCodeFlag,
			utils.ExcludeStorageFlag,
			utils.IncludeIncompletesFlag,
			utils.StartKeyFlag,
			utils.DumpLimitFlag,
		}, utils.DatabaseFlags),
		Description: `
This command dumps out the state for a given block (or latest, if none provided).
`,
	}

	pruneHistoryCommand = &cli.Command{
		Action:    pruneHistory,
		Name:      "prune-history",
		Usage:     "Prune blockchain history (block bodies and receipts) up to the merge block",
		ArgsUsage: "",
		Flags:     utils.DatabaseFlags,
		Description: `
The prune-history command removes historical block bodies and receipts from the
blockchain database up to the merge block, while preserving block headers. This
helps reduce storage requirements for nodes that don't need full historical data.`,
	}

	downloadEraCommand = &cli.Command{
		Action:    downloadEra,
		Name:      "download-era",
		Usage:     "Fetches era1 files (pre-merge history) from an HTTP endpoint",
		ArgsUsage: "",
		Flags: slices.Concat(
			utils.DatabaseFlags,
			utils.NetworkFlags,
			[]cli.Flag{
				eraBlockFlag,
				eraEpochFlag,
				eraAllFlag,
				eraServerFlag,
			},
		),
	}

	exportFromFirehoseCommand = &cli.Command{
		Action:    exportFromFirehose,
		Name:      "export-from-firehose",
		Usage:     "Export blocks from a Firehose gRPC endpoint to an RLP file",
		ArgsUsage: "<firehose-endpoint>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "batch-size",
				Usage: "Number of blocks per RLP file batch",
				Value: 1000,
			},
			&cli.StringFlag{
				Name:  "output",
				Usage: "Output file prefix (will append .rlp, .rlp.1, etc.)",
				Value: "firehose_export",
			},
			&cli.Int64Flag{
				Name:  "start-block",
				Usage: "Start block number (inclusive)",
			},
			&cli.Uint64Flag{
				Name:  "end-block",
				Usage: "End block number (inclusive, default: unlimited)",
				Value: 0,
			},
		},
		Description: `
Connects to a Firehose gRPC endpoint, streams Ethereum blocks, batches them, and writes them in RLP format compatible with 'geth import'.

Authentication: The Firehose endpoint may require an API token. By default, this command will look for the token in the FIREHOSE_API_TOKEN environment variable. You can override this by using the --api-token-env flag to specify a different environment variable name.
`,
	}
)

var (
	eraBlockFlag = &cli.StringFlag{
		Name:  "block",
		Usage: "Block number to fetch. (can also be a range <start>-<end>)",
	}
	eraEpochFlag = &cli.StringFlag{
		Name:  "epoch",
		Usage: "Epoch number to fetch (can also be a range <start>-<end>)",
	}
	eraAllFlag = &cli.BoolFlag{
		Name:  "all",
		Usage: "Download all available era1 files",
	}
	eraServerFlag = &cli.StringFlag{
		Name:  "server",
		Usage: "era1 server URL",
	}
)

// initGenesis will initialise the given JSON format genesis file and writes it as
// the zero'd block (i.e. genesis) or will fail hard if it can't succeed.
func initGenesis(ctx *cli.Context) error {
	if ctx.Args().Len() != 1 {
		utils.Fatalf("need genesis.json file as the only argument")
	}
	genesisPath := ctx.Args().First()
	if len(genesisPath) == 0 {
		utils.Fatalf("invalid path to genesis file")
	}
	file, err := os.Open(genesisPath)
	if err != nil {
		utils.Fatalf("Failed to read genesis file: %v", err)
	}
	defer file.Close()

	genesis := new(core.Genesis)
	if err := json.NewDecoder(file).Decode(genesis); err != nil {
		utils.Fatalf("invalid genesis file: %v", err)
	}
	// Open and initialise both full and light databases
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	var overrides core.ChainOverrides
	if ctx.IsSet(utils.OverridePrague.Name) {
		v := ctx.Uint64(utils.OverridePrague.Name)
		overrides.OverridePrague = &v
	}
	if ctx.IsSet(utils.OverrideVerkle.Name) {
		v := ctx.Uint64(utils.OverrideVerkle.Name)
		overrides.OverrideVerkle = &v
	}

	chaindb := utils.MakeChainDatabase(ctx, stack, false)
	defer chaindb.Close()

	triedb := utils.MakeTrieDatabase(ctx, chaindb, ctx.Bool(utils.CachePreimagesFlag.Name), false, genesis.IsVerkle())
	defer triedb.Close()

	_, hash, compatErr, err := core.SetupGenesisBlockWithOverride(chaindb, triedb, genesis, &overrides)
	if err != nil {
		utils.Fatalf("Failed to write genesis block: %v", err)
	}
	if compatErr != nil {
		utils.Fatalf("Failed to write chain config: %v", compatErr)
	}
	log.Info("Successfully wrote genesis state", "database", "chaindata", "hash", hash)

	return nil
}

func dumpGenesis(ctx *cli.Context) error {
	// check if there is a testnet preset enabled
	var genesis *core.Genesis
	if utils.IsNetworkPreset(ctx) {
		genesis = utils.MakeGenesis(ctx)
	} else if ctx.IsSet(utils.DeveloperFlag.Name) && !ctx.IsSet(utils.DataDirFlag.Name) {
		genesis = core.DeveloperGenesisBlock(11_500_000, nil)
	}

	if genesis != nil {
		if err := json.NewEncoder(os.Stdout).Encode(genesis); err != nil {
			utils.Fatalf("could not encode genesis: %s", err)
		}
		return nil
	}

	// dump whatever already exists in the datadir
	stack, _ := makeConfigNode(ctx)

	db, err := stack.OpenDatabaseWithOptions("chaindata", node.DatabaseOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer db.Close()

	genesis, err = core.ReadGenesis(db)
	if err != nil {
		utils.Fatalf("failed to read genesis: %s", err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(*genesis); err != nil {
		utils.Fatalf("could not encode stored genesis: %s", err)
	}

	return nil
}

func importChain(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}
	stack, cfg := makeConfigNode(ctx)
	defer stack.Close()

	// Start metrics export if enabled
	utils.SetupMetrics(&cfg.Metrics)

	chain, db := utils.MakeChain(ctx, stack, false)
	defer db.Close()

	// Start periodically gathering memory profiles
	var peakMemAlloc, peakMemSys atomic.Uint64
	go func() {
		stats := new(runtime.MemStats)
		for {
			runtime.ReadMemStats(stats)
			if peakMemAlloc.Load() < stats.Alloc {
				peakMemAlloc.Store(stats.Alloc)
			}
			if peakMemSys.Load() < stats.Sys {
				peakMemSys.Store(stats.Sys)
			}
			time.Sleep(5 * time.Second)
		}
	}()
	// Import the chain
	start := time.Now()

	var importErr error

	if ctx.Args().Len() == 1 {
		if err := utils.ImportChain(chain, ctx.Args().First()); err != nil {
			importErr = err
			log.Error("Import error", "err", err)
		}
	} else {
		for _, arg := range ctx.Args().Slice() {
			if err := utils.ImportChain(chain, arg); err != nil {
				importErr = err
				log.Error("Import error", "file", arg, "err", err)
				if err == utils.ErrImportInterrupted {
					break
				}
			}
		}
	}
	chain.Stop()
	fmt.Printf("Import done in %v.\n\n", time.Since(start))

	// Output pre-compaction stats mostly to see the import trashing
	showDBStats(db)

	// Print the memory statistics used by the importing
	mem := new(runtime.MemStats)
	runtime.ReadMemStats(mem)

	fmt.Printf("Object memory: %.3f MB current, %.3f MB peak\n", float64(mem.Alloc)/1024/1024, float64(peakMemAlloc.Load())/1024/1024)
	fmt.Printf("System memory: %.3f MB current, %.3f MB peak\n", float64(mem.Sys)/1024/1024, float64(peakMemSys.Load())/1024/1024)
	fmt.Printf("Allocations:   %.3f million\n", float64(mem.Mallocs)/1000000)
	fmt.Printf("GC pause:      %v\n\n", time.Duration(mem.PauseTotalNs))

	if ctx.Bool(utils.NoCompactionFlag.Name) {
		return nil
	}

	// Compact the entire database to more accurately measure disk io and print the stats
	start = time.Now()
	fmt.Println("Compacting entire database...")
	if err := db.Compact(nil, nil); err != nil {
		utils.Fatalf("Compaction failed: %v", err)
	}
	fmt.Printf("Compaction done in %v.\n\n", time.Since(start))

	showDBStats(db)
	return importErr
}

func exportChain(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chain, db := utils.MakeChain(ctx, stack, true)
	defer db.Close()
	start := time.Now()

	var err error
	fp := ctx.Args().First()
	if ctx.Args().Len() < 3 {
		err = utils.ExportChain(chain, fp)
	} else {
		// This can be improved to allow for numbers larger than 9223372036854775807
		first, ferr := strconv.ParseInt(ctx.Args().Get(1), 10, 64)
		last, lerr := strconv.ParseInt(ctx.Args().Get(2), 10, 64)
		if ferr != nil || lerr != nil {
			utils.Fatalf("Export error in parsing parameters: block number not an integer\n")
		}
		if first < 0 || last < 0 {
			utils.Fatalf("Export error: block number must be greater than 0\n")
		}
		if head := chain.CurrentSnapBlock(); uint64(last) > head.Number.Uint64() {
			utils.Fatalf("Export error: block number %d larger than head block %d\n", uint64(last), head.Number.Uint64())
		}
		err = utils.ExportAppendChain(chain, fp, uint64(first), uint64(last))
	}
	if err != nil {
		utils.Fatalf("Export error: %v\n", err)
	}
	fmt.Printf("Export done in %v\n", time.Since(start))
	return nil
}

func importHistory(ctx *cli.Context) error {
	if ctx.Args().Len() != 1 {
		utils.Fatalf("usage: %s", ctx.Command.ArgsUsage)
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chain, db := utils.MakeChain(ctx, stack, false)
	defer db.Close()

	var (
		start   = time.Now()
		dir     = ctx.Args().Get(0)
		network string
	)

	// Determine network.
	if utils.IsNetworkPreset(ctx) {
		switch {
		case ctx.Bool(utils.MainnetFlag.Name):
			network = "mainnet"
		case ctx.Bool(utils.SepoliaFlag.Name):
			network = "sepolia"
		case ctx.Bool(utils.HoleskyFlag.Name):
			network = "holesky"
		case ctx.Bool(utils.HoodiFlag.Name):
			network = "hoodi"
		}
	} else {
		// No network flag set, try to determine network based on files
		// present in directory.
		var networks []string
		for _, n := range params.NetworkNames {
			entries, err := era.ReadDir(dir, n)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", dir, err)
			}
			if len(entries) > 0 {
				networks = append(networks, n)
			}
		}
		if len(networks) == 0 {
			return fmt.Errorf("no era1 files found in %s", dir)
		}
		if len(networks) > 1 {
			return errors.New("multiple networks found, use a network flag to specify desired network")
		}
		network = networks[0]
	}

	if err := utils.ImportHistory(chain, dir, network); err != nil {
		return err
	}
	fmt.Printf("Import done in %v\n", time.Since(start))
	return nil
}

// exportHistory exports chain history in Era archives at a specified
// directory.
func exportHistory(ctx *cli.Context) error {
	if ctx.Args().Len() != 3 {
		utils.Fatalf("usage: %s", ctx.Command.ArgsUsage)
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chain, _ := utils.MakeChain(ctx, stack, true)
	start := time.Now()

	var (
		dir         = ctx.Args().Get(0)
		first, ferr = strconv.ParseInt(ctx.Args().Get(1), 10, 64)
		last, lerr  = strconv.ParseInt(ctx.Args().Get(2), 10, 64)
	)
	if ferr != nil || lerr != nil {
		utils.Fatalf("Export error in parsing parameters: block number not an integer\n")
	}
	if first < 0 || last < 0 {
		utils.Fatalf("Export error: block number must be greater than 0\n")
	}
	if head := chain.CurrentSnapBlock(); uint64(last) > head.Number.Uint64() {
		utils.Fatalf("Export error: block number %d larger than head block %d\n", uint64(last), head.Number.Uint64())
	}
	err := utils.ExportHistory(chain, dir, uint64(first), uint64(last), uint64(era.MaxEra1Size))
	if err != nil {
		utils.Fatalf("Export error: %v\n", err)
	}
	fmt.Printf("Export done in %v\n", time.Since(start))
	return nil
}

// importPreimages imports preimage data from the specified file.
// it is deprecated, and the export function has been removed, but
// the import function is kept around for the time being so that
// older file formats can still be imported.
func importPreimages(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false)
	defer db.Close()
	start := time.Now()

	if err := utils.ImportPreimages(db, ctx.Args().First()); err != nil {
		utils.Fatalf("Import error: %v\n", err)
	}
	fmt.Printf("Import done in %v\n", time.Since(start))
	return nil
}

func parseDumpConfig(ctx *cli.Context, db ethdb.Database) (*state.DumpConfig, common.Hash, error) {
	var header *types.Header
	if ctx.NArg() > 1 {
		return nil, common.Hash{}, fmt.Errorf("expected 1 argument (number or hash), got %d", ctx.NArg())
	}
	if ctx.NArg() == 1 {
		arg := ctx.Args().First()
		if hashish(arg) {
			hash := common.HexToHash(arg)
			if number := rawdb.ReadHeaderNumber(db, hash); number != nil {
				header = rawdb.ReadHeader(db, hash, *number)
			} else {
				return nil, common.Hash{}, fmt.Errorf("block %x not found", hash)
			}
		} else {
			number, err := strconv.ParseUint(arg, 10, 64)
			if err != nil {
				return nil, common.Hash{}, err
			}
			if hash := rawdb.ReadCanonicalHash(db, number); hash != (common.Hash{}) {
				header = rawdb.ReadHeader(db, hash, number)
			} else {
				return nil, common.Hash{}, fmt.Errorf("header for block %d not found", number)
			}
		}
	} else {
		// Use latest
		header = rawdb.ReadHeadHeader(db)
	}
	if header == nil {
		return nil, common.Hash{}, errors.New("no head block found")
	}
	startArg := common.FromHex(ctx.String(utils.StartKeyFlag.Name))
	var start common.Hash
	switch len(startArg) {
	case 0: // common.Hash
	case 32:
		start = common.BytesToHash(startArg)
	case 20:
		start = crypto.Keccak256Hash(startArg)
		log.Info("Converting start-address to hash", "address", common.BytesToAddress(startArg), "hash", start.Hex())
	default:
		return nil, common.Hash{}, fmt.Errorf("invalid start argument: %x. 20 or 32 hex-encoded bytes required", startArg)
	}
	conf := &state.DumpConfig{
		SkipCode:          ctx.Bool(utils.ExcludeCodeFlag.Name),
		SkipStorage:       ctx.Bool(utils.ExcludeStorageFlag.Name),
		OnlyWithAddresses: !ctx.Bool(utils.IncludeIncompletesFlag.Name),
		Start:             start.Bytes(),
		Max:               ctx.Uint64(utils.DumpLimitFlag.Name),
	}
	log.Info("State dump configured", "block", header.Number, "hash", header.Hash().Hex(),
		"skipcode", conf.SkipCode, "skipstorage", conf.SkipStorage,
		"start", hexutil.Encode(conf.Start), "limit", conf.Max)
	return conf, header.Root, nil
}

func dump(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true)
	defer db.Close()

	conf, root, err := parseDumpConfig(ctx, db)
	if err != nil {
		return err
	}
	triedb := utils.MakeTrieDatabase(ctx, db, true, true, false) // always enable preimage lookup
	defer triedb.Close()

	state, err := state.New(root, state.NewDatabase(triedb, nil))
	if err != nil {
		return err
	}
	if ctx.Bool(utils.IterativeOutputFlag.Name) {
		state.IterativeDump(conf, json.NewEncoder(os.Stdout))
	} else {
		fmt.Println(string(state.Dump(conf)))
	}
	return nil
}

// hashish returns true for strings that look like hashes.
func hashish(x string) bool {
	_, err := strconv.Atoi(x)
	return err != nil
}

func pruneHistory(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	// Open the chain database
	chain, chaindb := utils.MakeChain(ctx, stack, false)
	defer chaindb.Close()
	defer chain.Stop()

	// Determine the prune point. This will be the first PoS block.
	prunePoint, ok := history.PrunePoints[chain.Genesis().Hash()]
	if !ok || prunePoint == nil {
		return errors.New("prune point not found")
	}
	var (
		mergeBlock     = prunePoint.BlockNumber
		mergeBlockHash = prunePoint.BlockHash.Hex()
	)

	// Check we're far enough past merge to ensure all data is in freezer
	currentHeader := chain.CurrentHeader()
	if currentHeader == nil {
		return errors.New("current header not found")
	}
	if currentHeader.Number.Uint64() < mergeBlock+params.FullImmutabilityThreshold {
		return fmt.Errorf("chain not far enough past merge block, need %d more blocks",
			mergeBlock+params.FullImmutabilityThreshold-currentHeader.Number.Uint64())
	}

	// Double-check the prune block in db has the expected hash.
	hash := rawdb.ReadCanonicalHash(chaindb, mergeBlock)
	if hash != common.HexToHash(mergeBlockHash) {
		return fmt.Errorf("merge block hash mismatch: got %s, want %s", hash.Hex(), mergeBlockHash)
	}

	log.Info("Starting history pruning", "head", currentHeader.Number, "tail", mergeBlock, "tailHash", mergeBlockHash)
	start := time.Now()
	rawdb.PruneTransactionIndex(chaindb, mergeBlock)
	if _, err := chaindb.TruncateTail(mergeBlock); err != nil {
		return fmt.Errorf("failed to truncate ancient data: %v", err)
	}
	log.Info("History pruning completed", "tail", mergeBlock, "elapsed", common.PrettyDuration(time.Since(start)))

	// TODO(s1na): what if there is a crash between the two prune operations?

	return nil
}

// downladEra is the era1 file downloader tool.
func downloadEra(ctx *cli.Context) error {
	flags.CheckExclusive(ctx, eraBlockFlag, eraEpochFlag, eraAllFlag)

	// Resolve the network.
	var network = "mainnet"
	if utils.IsNetworkPreset(ctx) {
		switch {
		case ctx.IsSet(utils.MainnetFlag.Name):
		case ctx.IsSet(utils.SepoliaFlag.Name):
			network = "sepolia"
		default:
			return fmt.Errorf("unsupported network, no known era1 checksums")
		}
	}

	// Resolve the destination directory.
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	ancients := stack.ResolveAncient("chaindata", "")
	dir := filepath.Join(ancients, rawdb.ChainFreezerName, "era")
	if ctx.IsSet(utils.EraFlag.Name) {
		dir = filepath.Join(ancients, ctx.String(utils.EraFlag.Name))
	}

	baseURL := ctx.String(eraServerFlag.Name)
	if baseURL == "" {
		return fmt.Errorf("need --%s flag to download", eraServerFlag.Name)
	}

	l, err := eradl.New(baseURL, network)
	if err != nil {
		return err
	}
	switch {
	case ctx.IsSet(eraAllFlag.Name):
		return l.DownloadAll(dir)

	case ctx.IsSet(eraBlockFlag.Name):
		s := ctx.String(eraBlockFlag.Name)
		start, end, ok := parseRange(s)
		if !ok {
			return fmt.Errorf("invalid block range: %q", s)
		}
		return l.DownloadBlockRange(start, end, dir)

	case ctx.IsSet(eraEpochFlag.Name):
		s := ctx.String(eraEpochFlag.Name)
		start, end, ok := parseRange(s)
		if !ok {
			return fmt.Errorf("invalid epoch range: %q", s)
		}
		return l.DownloadEpochRange(start, end, dir)

	default:
		return fmt.Errorf("specify one of --%s, --%s, or --%s to download", eraAllFlag.Name, eraBlockFlag.Name, eraEpochFlag.Name)
	}
}

func parseRange(s string) (start uint64, end uint64, ok bool) {
	log.Info("Parsing block range", "input", s)
	if m, _ := regexp.MatchString("^[0-9]+-[0-9]+$", s); m {
		s1, s2, _ := strings.Cut(s, "-")
		start, err := strconv.ParseUint(s1, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		end, err = strconv.ParseUint(s2, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		if start > end {
			return 0, 0, false
		}
		log.Info("Parsing block range", "start", start, "end", end)
		return start, end, true
	}
	if m, _ := regexp.MatchString("^[0-9]+$", s); m {
		start, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		end = start
		log.Info("Parsing single block range", "block", start)
		return start, end, true
	}
	return 0, 0, false
}

// exportFromFirehose is the handler for the export-from-firehose command.
func exportFromFirehose(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		return fmt.Errorf("missing required <firehose-endpoint> argument")
	}
	if !ctx.IsSet("start-block") {
		return fmt.Errorf("missing required --start-block flag")
	}

	apiToken := os.Getenv("FIREHOSE_API_TOKEN")
	endpoint := ctx.Args().First()
	batchSize := ctx.Int("batch-size")
	outputPrefix := ctx.String("output")
	startBlock := ctx.Int64("start-block")
	endBlock := ctx.Uint64("end-block")

	client, closeFunc, grpcOpts, err := client.NewFirehoseClient(endpoint, apiToken, "", false, false)
	if err != nil {
		return fmt.Errorf("failed to create Firehose client: %w", err)
	}
	defer closeFunc()
	grpcOpts = append(grpcOpts, grpc.UseCompressor(gzip.Name))

	stream, err := client.Blocks(context.Background(), &pbfirehose.Request{
		StartBlockNum: startBlock,
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

	// Channels for pipelining
	rawCh := make(chan seqResponse, 100)
	blockCh := make(chan seqBlock, 100)
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
	workerCount := 100
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for sr := range rawCh {
				ethBlock := &pbeth.Block{}
				if err := sr.resp.Block.UnmarshalTo(ethBlock); err != nil {
					fmt.Printf("failed to unmarshal block: %v\n", err)
					continue
				}
				block, err := convertFirehoseBlockToGethBlock(ethBlock)
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

	// Stage 3: Batching and writing in order
	go func() {
		var (
			blocks      []*types.Block
			batchNum    int
			totalBlocks int
			writeErr    error
			nextSeq     uint64 = 0
			buffer             = make(map[uint64]*types.Block)
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
				totalBlocks++
				delete(buffer, nextSeq)
				nextSeq++
				if len(blocks) >= batchSize {
					if err := writeBatch(blocks, outputPrefix, batchNum); err != nil {
						writeErr = err
						break
					}
					fmt.Printf("Wrote batch %d with %d blocks (total: %d, last block: %d)\n", batchNum, len(blocks), totalBlocks, block.NumberU64())
					blocks = blocks[:0]
					batchNum++
				}
			}
			if writeErr != nil {
				break
			}
		}
		// Write any remaining blocks
		if len(blocks) > 0 && writeErr == nil {
			if err := writeBatch(blocks, outputPrefix, batchNum); err != nil {
				writeErr = err
			} else {
				fmt.Printf("Wrote final batch %d with %d blocks\n", batchNum, len(blocks))
			}
		}
		if writeErr != nil {
			errCh <- writeErr
		}
		close(doneCh)
	}()

	// Wait for completion or error
	select {
	case err := <-errCh:
		return err
	case <-doneCh:
		fmt.Println("Export completed successfully.")
		return nil
	}
}

// convertFirehoseBlockToGethBlock converts a Firehose protobuf block to a geth Block
func convertFirehoseBlockToGethBlock(pbBlock *pbeth.Block) (*types.Block, error) {
	if pbBlock == nil || pbBlock.Header == nil {
		return nil, fmt.Errorf("invalid block or header")
	}

	// Convert header
	header := &types.Header{
		ParentHash:  common.Hash(pbBlock.Header.ParentHash),
		UncleHash:   common.Hash(pbBlock.Header.UncleHash),
		Coinbase:    common.Address(pbBlock.Header.Coinbase),
		Root:        common.Hash(pbBlock.Header.StateRoot),
		TxHash:      common.Hash(pbBlock.Header.TransactionsRoot),
		ReceiptHash: common.Hash(pbBlock.Header.ReceiptRoot),
		Bloom:       types.BytesToBloom(pbBlock.Header.LogsBloom),
		Difficulty:  pbBlock.Header.Difficulty.Native(),
		Number:      big.NewInt(int64(pbBlock.Header.Number)),
		GasLimit:    pbBlock.Header.GasLimit,
		GasUsed:     pbBlock.Header.GasUsed,
		Time:        uint64(pbBlock.Header.Timestamp.Seconds),
		Extra:       pbBlock.Header.ExtraData,
		MixDigest:   common.Hash(pbBlock.Header.MixHash),
		Nonce:       types.EncodeNonce(pbBlock.Header.Nonce),
	}

	if pbBlock.Header.BaseFeePerGas != nil {
		header.BaseFee = pbBlock.Header.BaseFeePerGas.Native()
	}

	if pbBlock.Header.WithdrawalsRoot != nil {
		header.WithdrawalsHash = (*common.Hash)(pbBlock.Header.WithdrawalsRoot)
	}

	if pbBlock.Header.BlobGasUsed != nil {
		header.BlobGasUsed = pbBlock.Header.BlobGasUsed
	}

	if pbBlock.Header.ExcessBlobGas != nil {
		header.ExcessBlobGas = pbBlock.Header.ExcessBlobGas
	}

	if pbBlock.Header.ParentBeaconRoot != nil {
		header.ParentBeaconRoot = (*common.Hash)(pbBlock.Header.ParentBeaconRoot)
	}

	if pbBlock.Header.RequestsHash != nil {
		header.ParentBeaconRoot = (*common.Hash)(pbBlock.Header.RequestsHash)
	}

	// Convert transactions
	var txs []*types.Transaction
	var receipts types.Receipts
	for _, pbTx := range pbBlock.TransactionTraces {
		if pbTx == nil {
			continue
		}

		var tx *types.Transaction
		switch pbTx.Type {
		case 0: // LegacyTx
			tx = types.NewTx(&types.LegacyTx{
				Nonce:    pbTx.Nonce,
				GasPrice: pbTx.GasPrice.Native(),
				Gas:      pbTx.GasLimit,
				To:       func() *common.Address { addr := common.BytesToAddress(pbTx.To); return &addr }(),
				Value:    pbTx.Value.Native(),
				Data:     pbTx.Input,
				V:        new(big.Int).SetBytes(pbTx.V),
				R:        new(big.Int).SetBytes(pbTx.R),
				S:        new(big.Int).SetBytes(pbTx.S),
			})
		case 1: // AccessListTx
			tx = types.NewTx(&types.AccessListTx{
				ChainID:    extractChainIDFromSetCodeAuth(pbTx.SetCodeAuthorizations).ToBig(),
				Nonce:      pbTx.Nonce,
				GasPrice:   pbTx.GasPrice.Native(),
				Gas:        pbTx.GasLimit,
				To:         func() *common.Address { addr := common.BytesToAddress(pbTx.To); return &addr }(),
				Value:      pbTx.Value.Native(),
				Data:       pbTx.Input,
				AccessList: convertFirehoseAccessList(pbTx.AccessList),
				V:          new(big.Int).SetBytes(pbTx.V),
				R:          new(big.Int).SetBytes(pbTx.R),
				S:          new(big.Int).SetBytes(pbTx.S),
			})
		case 2: // DynamicFeeTx
			tx = types.NewTx(&types.DynamicFeeTx{
				ChainID:    extractChainIDFromSetCodeAuth(pbTx.SetCodeAuthorizations).ToBig(),
				Nonce:      pbTx.Nonce,
				GasTipCap:  pbTx.MaxPriorityFeePerGas.Native(),
				GasFeeCap:  pbTx.MaxFeePerGas.Native(),
				Gas:        pbTx.GasLimit,
				To:         func() *common.Address { addr := common.BytesToAddress(pbTx.To); return &addr }(),
				Value:      pbTx.Value.Native(),
				Data:       pbTx.Input,
				AccessList: convertFirehoseAccessList(pbTx.AccessList),
				V:          new(big.Int).SetBytes(pbTx.V),
				R:          new(big.Int).SetBytes(pbTx.R),
				S:          new(big.Int).SetBytes(pbTx.S),
			})
		case 3: // BlobTx
			tx = types.NewTx(&types.BlobTx{
				ChainID:    extractChainIDFromSetCodeAuth(pbTx.SetCodeAuthorizations),
				Nonce:      pbTx.Nonce,
				GasTipCap:  bigIntToUint256(pbTx.MaxPriorityFeePerGas.Native()),
				GasFeeCap:  bigIntToUint256(pbTx.MaxFeePerGas.Native()),
				Gas:        pbTx.GasLimit,
				To:         common.BytesToAddress(pbTx.To),
				Value:      bigIntToUint256(pbTx.Value.Native()),
				Data:       pbTx.Input,
				AccessList: convertFirehoseAccessList(pbTx.AccessList),
				BlobFeeCap: bigIntToUint256(pbTx.BlobGasFeeCap.Native()),
				BlobHashes: convertFirehoseBlobHashes(pbTx.BlobHashes),
				V:          bigIntToUint256(new(big.Int).SetBytes(pbTx.V)),
				R:          bigIntToUint256(new(big.Int).SetBytes(pbTx.R)),
				S:          bigIntToUint256(new(big.Int).SetBytes(pbTx.S)),
			})
		case 4: // SetCodeTx
			tx = types.NewTx(&types.SetCodeTx{
				ChainID:    extractChainIDFromSetCodeAuth(pbTx.SetCodeAuthorizations),
				Nonce:      pbTx.Nonce,
				GasTipCap:  bigIntToUint256(pbTx.MaxPriorityFeePerGas.Native()),
				GasFeeCap:  bigIntToUint256(pbTx.MaxFeePerGas.Native()),
				Gas:        pbTx.GasLimit,
				To:         common.BytesToAddress(pbTx.To),
				Value:      bigIntToUint256(pbTx.Value.Native()),
				Data:       pbTx.Input,
				AccessList: convertFirehoseAccessList(pbTx.AccessList),
				AuthList:   convertFirehoseSetCodeAuthorizations(pbTx.SetCodeAuthorizations),
				V:          bigIntToUint256(new(big.Int).SetBytes(pbTx.V)),
				R:          bigIntToUint256(new(big.Int).SetBytes(pbTx.R)),
				S:          bigIntToUint256(new(big.Int).SetBytes(pbTx.S)),
			})
		default:
			continue
		}

		if tx != nil {
			txs = append(txs, tx)
		}

		// Convert receipt if present
		if pbTx.Receipt != nil {
			receipt := &types.Receipt{
				Type:              uint8(pbTx.Type),
				PostState:         pbTx.Receipt.StateRoot,
				CumulativeGasUsed: pbTx.Receipt.CumulativeGasUsed,
				Bloom:             types.BytesToBloom(pbTx.Receipt.LogsBloom),
				Logs:              convertFirehoseLogsToGethLogs(pbTx.Receipt.Logs),

				TxHash:  common.Hash(pbTx.Hash),
				GasUsed: pbTx.GasUsed,
			}

			// Status
			if len(pbTx.Receipt.StateRoot) == 0 {
				if pbTx.Status == 1 {
					receipt.Status = types.ReceiptStatusSuccessful
				} else {
					receipt.Status = types.ReceiptStatusFailed
				}
			}

			// ContractAddress
			if pbTx.To == nil || len(pbTx.To) == 0 {
				// TODO
				receipt.ContractAddress = common.Address{}
			}

			// EffectiveGasPrice
			if pbTx.GasPrice != nil {
				// TODO
				receipt.EffectiveGasPrice = pbTx.GasPrice.Native()
			}

			// BlobGasUsed
			if pbTx.Receipt.BlobGasUsed != nil {
				receipt.BlobGasUsed = *pbTx.Receipt.BlobGasUsed
			}
			if pbTx.Receipt.BlobGasPrice != nil {
				receipt.BlobGasPrice = pbTx.Receipt.BlobGasPrice.Native()
			}

			// BlockHash, BlockNumber, TransactionIndex: not available in pbTx, set to zero/nil
			// These will be derived later by Receipts.DeriveFields
			// TODO
			receipt.BlockHash = common.Hash{}
			receipt.BlockNumber = nil
			receipt.TransactionIndex = 0
			receipts = append(receipts, receipt)
		}
	}

	// Convert uncles
	var uncles []*types.Header
	for _, pbUncle := range pbBlock.Uncles {
		if pbUncle == nil {
			continue
		}
		uncle := &types.Header{
			ParentHash:  common.Hash(pbUncle.ParentHash),
			UncleHash:   common.Hash(pbUncle.UncleHash),
			Coinbase:    common.Address(pbUncle.Coinbase),
			Root:        common.Hash(pbUncle.StateRoot),
			TxHash:      common.Hash(pbUncle.TransactionsRoot),
			ReceiptHash: common.Hash(pbUncle.ReceiptRoot),
			Bloom:       types.BytesToBloom(pbUncle.LogsBloom),
			Difficulty:  pbUncle.Difficulty.Native(),
			Number:      big.NewInt(int64(pbUncle.Number)),
			GasLimit:    pbUncle.GasLimit,
			GasUsed:     pbUncle.GasUsed,
			Time:        uint64(pbUncle.Timestamp.Seconds),
			Extra:       pbUncle.ExtraData,
			MixDigest:   common.Hash(pbUncle.MixHash),
			Nonce:       types.EncodeNonce(pbUncle.Nonce),
		}
		if pbUncle.BaseFeePerGas != nil {
			uncle.BaseFee = pbUncle.BaseFeePerGas.Native()
		}

		if pbUncle.WithdrawalsRoot != nil {
			uncle.WithdrawalsHash = (*common.Hash)(pbUncle.WithdrawalsRoot)
		}

		if pbUncle.BlobGasUsed != nil {
			uncle.BlobGasUsed = pbUncle.BlobGasUsed
		}

		if pbUncle.ExcessBlobGas != nil {
			uncle.ExcessBlobGas = pbUncle.ExcessBlobGas
		}

		if pbUncle.ParentBeaconRoot != nil {
			uncle.ParentBeaconRoot = (*common.Hash)(pbUncle.ParentBeaconRoot)
		}

		if pbUncle.RequestsHash != nil {
			uncle.ParentBeaconRoot = (*common.Hash)(pbUncle.RequestsHash)
		}

		uncles = append(uncles, uncle)
	}

	body := &types.Body{
		Transactions: txs,
		Uncles:       uncles,
		// TODO: Withdrawal
	}

	return types.NewBlock(header, body, receipts, trie.NewStackTrie(nil)), nil
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
