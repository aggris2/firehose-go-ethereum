package pulse

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func TestReplaceDepositContractDestroysOld(t *testing.T) {
	// Create state
	memDB := rawdb.NewMemoryDatabase()
	trieDB := triedb.NewDatabase(memDB, &triedb.Config{Preimages: true})
	stateDB := state.NewDatabase(trieDB, nil)
	stateObj, err := state.New(common.Hash{}, stateDB)
	if err != nil {
		t.Fatalf("Failed to create state: %v", err)
	}

	// Pre-populate old ETH deposit contract with code and balance
	stateObj.CreateAccount(ethereumDepositContractAddr)
	stateObj.AddBalance(ethereumDepositContractAddr, uint256.NewInt(1000000), tracing.BalanceIncreaseGenesisBalance)
	fakeCode := []byte{0x60, 0x80, 0x60, 0x40}
	stateObj.SetCode(ethereumDepositContractAddr, fakeCode, tracing.CodeChangeGenesis)

	// Verify pre-conditions
	if stateObj.GetBalance(ethereumDepositContractAddr).IsZero() {
		t.Fatal("old deposit contract should have balance before replace")
	}
	if len(stateObj.GetCode(ethereumDepositContractAddr)) == 0 {
		t.Fatal("old deposit contract should have code before replace")
	}

	// Execute
	replaceDepositContract(stateObj)

	// Verify old contract has nilContractBytes (not empty)
	oldCode := stateObj.GetCode(ethereumDepositContractAddr)
	if !bytes.Equal(oldCode, nilContractBytes) {
		t.Fatalf("old contract code: got %d bytes, want nilContractBytes (%d bytes)", len(oldCode), len(nilContractBytes))
	}

	// Verify old contract balance is 0
	if !stateObj.GetBalance(ethereumDepositContractAddr).IsZero() {
		t.Errorf("old contract balance should be 0, got %s", stateObj.GetBalance(ethereumDepositContractAddr))
	}
}

func TestReplaceDepositContract(t *testing.T) {
	// Init
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

	// Exec
	replaceDepositContract(stateObj)

	// Verify
	balance := stateObj.GetBalance(pulseDepositContractAddr)
	if balance.Cmp(uint256.NewInt(0)) != 0 {
		t.Errorf("Found unexpected deposit contract balance: %s", balance.String())
	}

	actualCode := stateObj.GetCode(pulseDepositContractAddr)
	if len(actualCode) != len(depositContractBytes) {
		t.Fatalf("Contract code length mismatch: got %d, want %d", len(actualCode), len(depositContractBytes))
	}

	for i, b := range actualCode {
		if b != depositContractBytes[i] {
			t.Errorf("Invalid deposit contract code at index %d", i)
			break
		}
	}

	// Verify Storage
	for i, store := range depositContractStorage {
		actualStorage := stateObj.GetState(pulseDepositContractAddr, common.HexToHash(store[0]))
		expectedStorage := common.HexToHash(store[1])
		if actualStorage != expectedStorage {
			t.Errorf("Invalid storage entry %d, actual: %s, expected: %s", i, actualStorage.String(), expectedStorage.String())
		} else {
			t.Logf("Valid Storage entry %d", i)
		}
	}
}
