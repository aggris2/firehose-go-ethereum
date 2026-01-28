package pulse

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func TestApplySacrificeCredits(t *testing.T) {
	// Init
	var pulseChainTestnetTreasuryBalance math.HexOrDecimal256
	pulseChainTestnetTreasuryBalance.UnmarshalText([]byte("0xC9F2C9CD04674EDEA40000000"))

	// Create a new memory database
	memDB := rawdb.NewMemoryDatabase()

	// Create a new triedb with the memory database
	trieDB := triedb.NewDatabase(memDB, &triedb.Config{Preimages: true})

	// Create a state database using the triedb
	stateDB := state.NewDatabase(trieDB, nil)

	// Create a new state with the empty root
	stateObj, err := state.New(common.Hash{}, stateDB)
	if err != nil {
		t.Fatalf("Failed to create state: %v", err)
	}

	// Create treasury
	treasury := &params.Treasury{
		Addr:    "0xceB59257450820132aB274ED61C49E5FD96E8868",
		Balance: &pulseChainTestnetTreasuryBalance,
	}

	// Exec
	applySacrificeCredits(stateObj, treasury, params.PulseChainConfig.ChainID)

	// Verify
	actual := stateObj.GetBalance(common.HexToAddress(treasury.Addr))
	expected := uint256.MustFromBig((*big.Int)(treasury.Balance))
	if actual.Cmp(expected) != 0 {
		t.Errorf("Invalid treasury balance, actual: %d, expected: %d", actual, expected)
	} else {
		t.Log("Treasury allocating successful")
	}

	// from the credits.csv file in compressed-allocations
	actual = stateObj.GetBalance(common.HexToAddress("0x000000005dCEE11e13fb536Fa40d65450F53c5a8"))
	bal, _ := new(big.Int).SetString("64000000000000000000", 10)
	expected = uint256.MustFromBig(bal)
	if actual.Cmp(expected) != 0 {
		t.Errorf("Invalid sacrifice credit balance, actual: %d, expected: %d", actual, expected)
	} else {
		t.Log("Sacrifice allocation successful")
	}
}
