package pulse

import (
	_ "embed"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// see https://gitlab.com/pulsechaincom/compressed-allocations/-/tags/Mainnet
//
//go:embed sacrifice_credits_mainnet.bin
var mainnetRawCredits []byte

// see https://gitlab.com/pulsechaincom/compressed-allocations/-/tags/Testnet-V4
//
//go:embed sacrifice_credits_testnet_v4.bin
var testnetV4RawCredits []byte

// Applies the sacrifice credits for the PrimordialPulse fork.
func applySacrificeCredits(state vm.StateDB, treasury *params.Treasury, chainID *big.Int) {
	rawCredits := mainnetRawCredits
	if chainID.Cmp(params.PulseChainTestnetV4Config.ChainID) == 0 {
		rawCredits = testnetV4RawCredits
	}

	if treasury != nil {
		log.Info("Applying PrimordialPulse treasury allocation 💸")
		treasuryAddr := common.HexToAddress(treasury.Addr)
		treasuryAmount := uint256.MustFromBig((*big.Int)(treasury.Balance))
		state.AddBalance(treasuryAddr, treasuryAmount, tracing.BalanceIncreaseGenesisBalance)
	}

	log.Info("Applying PrimordialPulse sacrifice credits 💸")
	for ptr := 0; ptr < len(rawCredits); {
		byteCount := int(rawCredits[ptr])
		ptr++
		record := rawCredits[ptr : ptr+byteCount]
		ptr += byteCount
		addr := common.BytesToAddress(record[:20])
		credit := new(uint256.Int).SetBytes(record[20:])
		state.AddBalance(addr, credit, tracing.BalanceIncreaseGenesisBalance)
	}
	log.Info("Finished applying PrimordialPulse sacrifice credits 🤑")
}
