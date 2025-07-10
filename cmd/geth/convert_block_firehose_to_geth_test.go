package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	pbeth "github.com/streamingfast/firehose-ethereum/types/pb/sf/ethereum/type/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var update = flag.Bool("update", false, "update golden files")

func TestConvertFirehoseBlockToGethBlock(t *testing.T) {
	tests := []struct {
		name    string
		pbBlock *pbeth.Block
		wantErr bool
	}{
		{
			name:    "nil block",
			pbBlock: nil,
			wantErr: true,
		},
		{
			name: "nil header",
			pbBlock: &pbeth.Block{
				Header: nil,
			},
			wantErr: true,
		},
		{
			name: "valid block",
			pbBlock: &pbeth.Block{
				Header: &pbeth.BlockHeader{
					ParentHash:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
					UncleHash:        []byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33},
					Coinbase:         []byte{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22},
					StateRoot:        []byte{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35},
					TransactionsRoot: []byte{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36},
					ReceiptRoot:      []byte{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37},
					LogsBloom:        make([]byte, 256),
					Difficulty:       &pbeth.BigInt{Bytes: []byte{100}},
					Number:           12345,
					GasLimit:         30000000,
					GasUsed:          15000000,
					Timestamp:        timestamppb.New(time.Unix(1634952202, 0)),
					ExtraData:        []byte{8, 9, 10},
					MixHash:          []byte{9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
					Nonce:            123456789,
					BaseFeePerGas:    &pbeth.BigInt{Bytes: big.NewInt(20000000000).Bytes()},
					WithdrawalsRoot:  []byte{101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132},
					BlobGasUsed:      new(uint64),
					ExcessBlobGas:    new(uint64),
					ParentBeaconRoot: []byte{201, 202, 203, 204, 205, 206, 207, 208, 209, 210, 211, 212, 213, 214, 215, 216, 217, 218, 219, 220, 221, 222, 223, 224, 225, 226, 227, 228, 229, 230, 231, 232},
					RequestsHash:     []byte{151, 152, 153, 154, 155, 156, 157, 158, 159, 160, 161, 162, 163, 164, 165, 166, 167, 168, 169, 170, 171, 172, 173, 174, 175, 176, 177, 178, 179, 180, 181, 182},
				},
				TransactionTraces: []*pbeth.TransactionTrace{
					{
						Type:     0, // LegacyTx
						Hash:     []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
						Nonce:    0,
						GasPrice: &pbeth.BigInt{Bytes: big.NewInt(20000000000).Bytes()},
						GasLimit: 21000,
						To:       []byte{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30},
						Value:    &pbeth.BigInt{Bytes: big.NewInt(1000000000000000000).Bytes()},
						Input:    []byte{},
						V:        []byte{27},
						R:        []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
						S:        []byte{33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64},
						Status:   1,
						GasUsed:  21000,
						Receipt: &pbeth.TransactionReceipt{
							StateRoot:         []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
							CumulativeGasUsed: 21000,
							LogsBloom:         make([]byte, 256), // Bloom filter is 256 bytes
							Logs:              []*pbeth.Log{},
						},
						MaxPriorityFeePerGas:  &pbeth.BigInt{Bytes: []byte{0}},
						MaxFeePerGas:          &pbeth.BigInt{Bytes: []byte{0}},
						AccessList:            []*pbeth.AccessTuple{},
						BlobGasFeeCap:         &pbeth.BigInt{Bytes: []byte{0}},
						BlobHashes:            [][]byte{},
						SetCodeAuthorizations: []*pbeth.SetCodeAuthorization{},
					},
				},
				Uncles: []*pbeth.BlockHeader{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, err := convertFirehoseBlockToGethBlock(tt.pbBlock)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertFirehoseBlockToGethBlock() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && block == nil {
				t.Error("convertFirehoseBlockToGethBlock() returned nil block when no error expected")
			}

			// Only do golden file comparison for the valid block case
			if tt.name == "valid block" && !tt.wantErr {
				type blockForGolden struct {
					Header       *types.Header
					Transactions []*types.Transaction
					Uncles       []*types.Header
				}
				golden := blockForGolden{
					Header:       block.Header(),
					Transactions: block.Transactions(),
					Uncles:       block.Uncles(),
				}
				got, err := json.MarshalIndent(golden, "", "  ")
				if err != nil {
					t.Fatalf("failed to marshal block: %v", err)
				}
				goldenFile := filepath.Join("testdata", "firehose_block.golden.json")
				if *update {
					if err := os.MkdirAll(filepath.Dir(goldenFile), 0755); err != nil {
						t.Fatalf("failed to create testdata dir: %v", err)
					}
					if err := ioutil.WriteFile(goldenFile, got, 0644); err != nil {
						t.Fatalf("failed to write golden file: %v", err)
					}
				} else {
					want, err := ioutil.ReadFile(goldenFile)
					if err != nil {
						t.Fatalf("failed to read golden file: %v", err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("block does not match golden file.\nGot:\n%s\nWant:\n%s", got, want)
					}
				}
			}
		})
	}
}
