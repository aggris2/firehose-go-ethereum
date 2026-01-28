package pulse

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func TestPrimordialPulseForkIntegration(t *testing.T) {
	// Setup state
	memDB := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(memDB, &triedb.Config{Preimages: true})
	stateDB := state.NewDatabase(trieDB, nil)
	stateObj, err := state.New(common.Hash{}, stateDB)
	if err != nil {
		t.Fatalf("Failed to create state: %v", err)
	}

	// Pre-populate old ETH deposit contract
	stateObj.CreateAccount(ethereumDepositContractAddr)
	stateObj.AddBalance(ethereumDepositContractAddr, uint256.NewInt(1_000_000), tracing.BalanceIncreaseGenesisBalance)
	stateObj.SetCode(ethereumDepositContractAddr, []byte{0x60, 0x80}, tracing.CodeChangeGenesis)

	// Setup treasury
	var treasuryBalance math.HexOrDecimal256
	treasuryBalance.UnmarshalText([]byte("0xC9F2C9CD04674EDEA40000000"))
	treasury := &params.Treasury{
		Addr:    "0xceB59257450820132aB274ED61C49E5FD96E8868",
		Balance: &treasuryBalance,
	}

	chainID := params.PulseChainConfig.ChainID

	// Execute
	PrimordialPulseFork(stateObj, treasury, chainID)

	// Subtest: old ETH deposit contract has nilContractBytes
	t.Run("old deposit contract destroyed", func(t *testing.T) {
		code := stateObj.GetCode(ethereumDepositContractAddr)
		if !bytes.Equal(code, nilContractBytes) {
			t.Errorf("old deposit contract should have nilContractBytes, got %d bytes", len(code))
		}
	})

	// Subtest: new PulseChain deposit contract deployed
	t.Run("new deposit contract deployed", func(t *testing.T) {
		code := stateObj.GetCode(pulseDepositContractAddr)
		if !bytes.Equal(code, depositContractBytes) {
			t.Errorf("new deposit contract code mismatch: got %d bytes, want %d bytes",
				len(code), len(depositContractBytes))
		}
	})

	// Subtest: treasury received balance
	t.Run("treasury balance set", func(t *testing.T) {
		bal := stateObj.GetBalance(common.HexToAddress(treasury.Addr))
		expected := uint256.MustFromBig((*big.Int)(treasury.Balance))
		if bal.Cmp(expected) != 0 {
			t.Errorf("treasury balance: got %s, want %s", bal, expected)
		}
	})

	// Subtest: sacrifice credit spot-check
	t.Run("sacrifice credit spot-check", func(t *testing.T) {
		bal := stateObj.GetBalance(common.HexToAddress("0x000000005dCEE11e13fb536Fa40d65450F53c5a8"))
		expectedBal, _ := new(big.Int).SetString("64000000000000000000", 10)
		expected := uint256.MustFromBig(expectedBal)
		if bal.Cmp(expected) != 0 {
			t.Errorf("sacrifice credit balance: got %s, want %s", bal, expected)
		}
	})
}
