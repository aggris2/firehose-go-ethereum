package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	pbeth "github.com/streamingfast/firehose-ethereum/types/pb/sf/ethereum/type/v2"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"
	"sync"
)

// convertFirehoseBlockToGethBlock converts a Firehose protobuf block to a geth Block
func convertFirehoseBlockToGethBlock(pbBlock *pbeth.Block, chainID *big.Int, jwt string, endpoint string) (*types.Block, error) {
	if pbBlock == nil || pbBlock.Header == nil {
		return nil, fmt.Errorf("invalid block or header")
	}

	// Convert header
	header := &types.Header{
		ParentHash:  common.BytesToHash(pbBlock.Header.ParentHash),
		UncleHash:   common.BytesToHash(pbBlock.Header.UncleHash),
		Coinbase:    common.BytesToAddress(pbBlock.Header.Coinbase),
		Root:        common.BytesToHash(pbBlock.Header.StateRoot),
		TxHash:      common.BytesToHash(pbBlock.Header.TransactionsRoot),
		ReceiptHash: common.BytesToHash(pbBlock.Header.ReceiptRoot),
		Bloom:       types.BytesToBloom(pbBlock.Header.LogsBloom),
		Difficulty:  pbBlock.Header.Difficulty.Native(),
		Number:      new(big.Int).SetUint64(pbBlock.Header.Number),
		GasLimit:    pbBlock.Header.GasLimit,
		GasUsed:     pbBlock.Header.GasUsed,
		Time:        uint64(pbBlock.Header.Timestamp.Seconds),
		Extra:       pbBlock.Header.ExtraData,
		MixDigest:   common.BytesToHash(pbBlock.Header.MixHash),
		Nonce: func() types.BlockNonce {
			return types.EncodeNonce(pbBlock.Header.Nonce)
		}(),
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
		header.RequestsHash = (*common.Hash)(pbBlock.Header.RequestsHash)
	}

	// Convert transactions
	var txs []*types.Transaction
	var receipts types.Receipts
	for txIndex, pbTx := range pbBlock.TransactionTraces {
		if pbTx == nil {
			continue
		}

		// Contract creation have "To" set to nil
		var toPtr *common.Address
		if len(pbTx.Calls) > 0 && pbTx.Calls[0].CallType == 5 {
			toPtr = nil
		} else {
			if len(pbTx.To) != 0 {
				addr := common.BytesToAddress(pbTx.To)
				toPtr = &addr
			} else {
				toPtr = nil
			}
		}

		var tx *types.Transaction
		switch pbTx.Type {
		case 0: // LegacyTx
			tx = types.NewTx(&types.LegacyTx{
				Nonce:    pbTx.Nonce,
				GasPrice: pbTx.GasPrice.Native(),
				Gas:      pbTx.GasLimit,
				To:       toPtr,
				Value:    pbTx.Value.Native(),
				Data:     pbTx.Input,
				V:        new(big.Int).SetBytes(pbTx.V),
				R:        new(big.Int).SetBytes(pbTx.R),
				S:        new(big.Int).SetBytes(pbTx.S),
			})
		case 1: // AccessListTx
			tx = types.NewTx(&types.AccessListTx{
				ChainID:    chainID,
				Nonce:      pbTx.Nonce,
				GasPrice:   pbTx.GasPrice.Native(),
				Gas:        pbTx.GasLimit,
				To:         toPtr,
				Value:      pbTx.Value.Native(),
				Data:       pbTx.Input,
				AccessList: convertFirehoseAccessList(pbTx.AccessList),
				V:          new(big.Int).SetBytes(pbTx.V),
				R:          new(big.Int).SetBytes(pbTx.R),
				S:          new(big.Int).SetBytes(pbTx.S),
			})
		case 2: // DynamicFeeTx
			tx = types.NewTx(&types.DynamicFeeTx{
				ChainID:    chainID,
				Nonce:      pbTx.Nonce,
				GasTipCap:  pbTx.MaxPriorityFeePerGas.Native(),
				GasFeeCap:  pbTx.MaxFeePerGas.Native(),
				Gas:        pbTx.GasLimit,
				To:         toPtr,
				Value:      pbTx.Value.Native(),
				Data:       pbTx.Input,
				AccessList: convertFirehoseAccessList(pbTx.AccessList),
				V:          new(big.Int).SetBytes(pbTx.V),
				R:          new(big.Int).SetBytes(pbTx.R),
				S:          new(big.Int).SetBytes(pbTx.S),
			})
		case 3: // BlobTx
			tx = types.NewTx(&types.BlobTx{
				ChainID:   uint256.MustFromBig(chainID),
				Nonce:     pbTx.Nonce,
				GasTipCap: bigIntToUint256(pbTx.MaxPriorityFeePerGas.Native()),
				GasFeeCap: bigIntToUint256(pbTx.MaxFeePerGas.Native()),
				Gas:       pbTx.GasLimit,
				To: func() common.Address {
					if pbTx.Status == 0 {
						return common.Address{}
					}
					return common.BytesToAddress(pbTx.To)
				}(),
				Value:      bigIntToUint256(pbTx.Value.Native()),
				Data:       pbTx.Input,
				AccessList: convertFirehoseAccessList(pbTx.AccessList),
				BlobFeeCap: bigIntToUint256(pbTx.BlobGasFeeCap.Native()),
				BlobHashes: convertBytesToHashes(pbTx.BlobHashes),
				V:          bigIntToUint256(new(big.Int).SetBytes(pbTx.V)),
				R:          bigIntToUint256(new(big.Int).SetBytes(pbTx.R)),
				S:          bigIntToUint256(new(big.Int).SetBytes(pbTx.S)),
			})
		case 4: // SetCodeTx
			tx = types.NewTx(&types.SetCodeTx{
				ChainID:   uint256.MustFromBig(chainID),
				Nonce:     pbTx.Nonce,
				GasTipCap: bigIntToUint256(pbTx.MaxPriorityFeePerGas.Native()),
				GasFeeCap: bigIntToUint256(pbTx.MaxFeePerGas.Native()),
				Gas:       pbTx.GasLimit,
				To: func() common.Address {
					if pbTx.Status == 0 {
						return common.Address{}
					}
					return common.BytesToAddress(pbTx.To)
				}(),
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

		// Convert receipt
		if pbTx.Receipt != nil {
			receipt := &types.Receipt{
				Type:              uint8(pbTx.Type),
				PostState:         pbTx.Receipt.StateRoot,
				CumulativeGasUsed: pbTx.Receipt.CumulativeGasUsed,
				Bloom:             types.BytesToBloom(pbTx.Receipt.LogsBloom),
				Logs:              convertFirehoseLogsToGethLogs(pbTx.Receipt.Logs, pbTx, pbBlock),

				TxHash:  common.BytesToHash(pbTx.Hash),
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
				receipt.ContractAddress = crypto.CreateAddress(common.BytesToAddress(pbTx.From), pbTx.Nonce)
			}

			// EffectiveGasPrice
			var effectiveGasPrice *big.Int
			switch pbTx.Type {
			case 0, 1: // Legacy & AccessList
				effectiveGasPrice = pbTx.GasPrice.Native()
			case 2, 3: // DynamicFee & Blob
				if pbTx.MaxFeePerGas != nil && pbTx.MaxPriorityFeePerGas != nil && pbBlock.Header.BaseFeePerGas != nil {
					baseFee := pbBlock.Header.BaseFeePerGas.Native()
					tip := pbTx.MaxPriorityFeePerGas.Native()
					maxFee := pbTx.MaxFeePerGas.Native()
					effectiveGasPrice = new(big.Int).Add(baseFee, tip)
					if effectiveGasPrice.Cmp(maxFee) > 0 {
						effectiveGasPrice = maxFee
					}
				}
			}
			receipt.EffectiveGasPrice = effectiveGasPrice

			// BlobGasUsed
			if pbTx.Receipt.BlobGasUsed != nil {
				receipt.BlobGasUsed = *pbTx.Receipt.BlobGasUsed
			}
			if pbTx.Receipt.BlobGasPrice != nil {
				receipt.BlobGasPrice = pbTx.Receipt.BlobGasPrice.Native()
			}

			receipt.BlockHash = header.Hash()
			receipt.BlockNumber = new(big.Int).SetUint64(pbBlock.Header.Number)
			receipt.TransactionIndex = uint(txIndex)
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
			ParentHash:  common.BytesToHash(pbUncle.ParentHash),
			UncleHash:   common.BytesToHash(pbUncle.UncleHash),
			Coinbase:    common.BytesToAddress(pbUncle.Coinbase),
			Root:        common.BytesToHash(pbUncle.StateRoot),
			TxHash:      common.BytesToHash(pbUncle.TransactionsRoot),
			ReceiptHash: common.BytesToHash(pbUncle.ReceiptRoot),
			Bloom:       types.BytesToBloom(pbUncle.LogsBloom),
			Difficulty:  pbUncle.Difficulty.Native(),
			Number:      big.NewInt(int64(pbUncle.Number)),
			GasLimit:    pbUncle.GasLimit,
			GasUsed:     pbUncle.GasUsed,
			Time:        uint64(pbUncle.Timestamp.Seconds),
			Extra:       pbUncle.ExtraData,
			MixDigest:   common.BytesToHash(pbUncle.MixHash),
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
			uncle.RequestsHash = (*common.Hash)(pbUncle.RequestsHash)
		}

		uncles = append(uncles, uncle)
	}

	body := &types.Body{
		Transactions: txs,
		Uncles:       uncles,
		Withdrawals:  createWithdrawals(pbBlock, jwt, endpoint),
	}

	return types.NewBlock(header, body, receipts, trie.NewStackTrie(nil)), nil
}

var withdrawalIndex uint64 = 0

// For ordered withdrawal assignment
var withdrawalOrderMu sync.Mutex
var withdrawalOrderCond = sync.NewCond(&withdrawalOrderMu)
var currentWithdrawalSeq uint64 = 0

// Helper to fetch validator indices for withdrawals from Alchemy
func fetchValidatorIndices(jwt string, blockNumber uint64, endpoint string) (map[uint64]uint64, error) {
	var url string
	if endpoint == "holesky.eth.streamingfast.io:443" {
		url = fmt.Sprintf("https://eth-holesky.g.alchemy.com/v2/%s", jwt)
	} else if endpoint == "hoodi.firehose.pinax.network:443" {
		url = fmt.Sprintf("https://eth-hoodi.g.alchemy.com/v2/%s", jwt)
	} else {
		return nil, fmt.Errorf("unsupported endpoint: %s", endpoint)
	}
	blockHex := fmt.Sprintf("0x%x", blockNumber)
	payload := fmt.Sprintf(`{
		"id": 1,
		"jsonrpc": "2.0",
		"method": "eth_getBlockByNumber",
		"params": ["%s", false]
	}`, blockHex)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		log.Warn("Non-200 response from validator index fetch", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("non-200 response: %d", resp.StatusCode)
	}

	// Parse the response
	var result struct {
		Result struct {
			Withdrawals []struct {
				Index          string `json:"index"`
				ValidatorIndex string `json:"validatorIndex"`
			} `json:"withdrawals"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// Build the map
	indexToValidator := make(map[uint64]uint64)
	for _, w := range result.Result.Withdrawals {
		idx, err1 := strconv.ParseUint(w.Index[2:], 16, 64)
		valIdx, err2 := strconv.ParseUint(w.ValidatorIndex[2:], 16, 64)
		if err1 == nil && err2 == nil {
			indexToValidator[idx] = valIdx
		}
	}
	return indexToValidator, nil
}

func createWithdrawals(block *pbeth.Block, jwt string, endpoint string) []*types.Withdrawal {
	if block.Header.WithdrawalsRoot == nil {
		return nil
	}

	withdrawals := []*types.Withdrawal{}
	validatorMap, err := fetchValidatorIndices(jwt, block.Number, endpoint)
	if err != nil {
		log.Warn("Could not fetch validator indices", "err", err)
	}
	for _, bc := range block.BalanceChanges {
		if bc.Reason == pbeth.BalanceChange_REASON_WITHDRAWAL {
			idx := withdrawalIndex
			validator := uint64(0)
			if v, ok := validatorMap[idx]; ok {
				validator = v
			}

			amount := new(big.Int).Sub(bc.NewValue.Native(), bc.OldValue.Native())
			gwei := new(big.Int).Div(amount, big.NewInt(1_000_000_000))

			withdrawal := &types.Withdrawal{
				Index:     idx,
				Validator: validator,
				Address:   common.BytesToAddress(bc.Address),
				Amount:    gwei.Uint64(),
			}
			withdrawals = append(withdrawals, withdrawal)
			withdrawalIndex++
		}
	}
	return withdrawals
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
			StorageKeys: convertBytesToHashes(tuple.StorageKeys),
		}
	}
	return alist
}

// Helper to convert Firehose SetCodeAuthorization to geth SetCodeAuthorization
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

// Helper to convert Firehose logs to geth logs
func convertFirehoseLogsToGethLogs(pbLogs []*pbeth.Log, pbTx *pbeth.TransactionTrace, pbBlock *pbeth.Block) []*types.Log {
	logs := make([]*types.Log, 0, len(pbLogs))
	for _, pbLog := range pbLogs {
		if pbLog == nil {
			continue
		}
		log := &types.Log{
			Address:        common.BytesToAddress(pbLog.Address),
			Topics:         convertBytesToHashes(pbLog.Topics),
			Data:           pbLog.Data,
			BlockNumber:    uint64(pbLog.BlockIndex),
			TxHash:         common.BytesToHash(pbTx.Hash),
			TxIndex:        uint(pbTx.Index),
			BlockHash:      common.BytesToHash(pbBlock.Header.Hash),
			BlockTimestamp: uint64(pbBlock.Header.Timestamp.Seconds),
			Index:          uint(pbLog.Index),
			Removed:        false,
		}

		if pbTx.Status == 3 { // Status Reverted
			log.Removed = true
		}
		logs = append(logs, log)
	}
	return logs
}

// Generic helper to convert [][]byte to []common.Hash
func convertBytesToHashes(pbBytes [][]byte) []common.Hash {
	if len(pbBytes) == 0 {
		return nil
	}
	hashes := make([]common.Hash, len(pbBytes))
	for i, b := range pbBytes {
		hashes[i] = common.BytesToHash(b)
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
