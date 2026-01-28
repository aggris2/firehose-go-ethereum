package ethash

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

// mockChainHeaderReader implements consensus.ChainHeaderReader for testing.
type mockChainHeaderReader struct {
	config *params.ChainConfig
}

func (m *mockChainHeaderReader) Config() *params.ChainConfig         { return m.config }
func (m *mockChainHeaderReader) CurrentHeader() *types.Header        { return nil }
func (m *mockChainHeaderReader) GetHeader(common.Hash, uint64) *types.Header { return nil }
func (m *mockChainHeaderReader) GetHeaderByNumber(uint64) *types.Header      { return nil }
func (m *mockChainHeaderReader) GetHeaderByHash(common.Hash) *types.Header   { return nil }

// pulseDepositContractAddr is the address of the PulseChain deposit contract.
var pulseDepositContractAddr = common.HexToAddress("0x3693693693693693693693693693693693693693")

func TestPulseChainForkBlockProcessing(t *testing.T) {
	var treasuryBalance math.HexOrDecimal256
	treasuryBalance.UnmarshalText([]byte("0xC9F2C9CD04674EDEA40000000"))

	config := &params.ChainConfig{
		ChainID:                 big.NewInt(369),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		PrimordialPulseBlock:    big.NewInt(100),
		TerminalTotalDifficulty: big.NewInt(0),
		Ethash:                  new(params.EthashConfig),
		Treasury: &params.Treasury{
			Addr:    "0xceB59257450820132aB274ED61C49E5FD96E8868",
			Balance: &treasuryBalance,
		},
	}

	chain := &mockChainHeaderReader{config: config}
	engine := NewFaker()

	newState := func() *state.StateDB {
		memDB := rawdb.NewMemoryDatabase()
		trieDB := triedb.NewDatabase(memDB, &triedb.Config{Preimages: true})
		stateDB := state.NewDatabase(trieDB, nil)
		s, err := state.New(common.Hash{}, stateDB)
		if err != nil {
			t.Fatalf("Failed to create state: %v", err)
		}
		return s
	}

	t.Run("finalize at fork block 100", func(t *testing.T) {
		s := newState()
		header := &types.Header{
			Number:     big.NewInt(100),
			Difficulty: big.NewInt(131072),
			Coinbase:   common.HexToAddress("0x1111111111111111111111111111111111111111"),
		}
		body := &types.Body{}
		engine.Finalize(chain, header, s, body)

		code := s.GetCode(pulseDepositContractAddr)
		if len(code) == 0 {
			t.Error("deposit contract should be deployed at fork block 100")
		}
	})

	t.Run("finalize at block 99 - no fork", func(t *testing.T) {
		s := newState()
		header := &types.Header{
			Number:     big.NewInt(99),
			Difficulty: big.NewInt(1000000),
			Coinbase:   common.HexToAddress("0x1111111111111111111111111111111111111111"),
		}
		body := &types.Body{}
		engine.Finalize(chain, header, s, body)

		code := s.GetCode(pulseDepositContractAddr)
		if len(code) != 0 {
			t.Error("deposit contract should NOT be deployed at block 99")
		}
	})

	t.Run("finalize at block 101 - no fork", func(t *testing.T) {
		s := newState()
		header := &types.Header{
			Number:     big.NewInt(101),
			Difficulty: big.NewInt(1000000),
			Coinbase:   common.HexToAddress("0x1111111111111111111111111111111111111111"),
		}
		body := &types.Body{}
		engine.Finalize(chain, header, s, body)

		code := s.GetCode(pulseDepositContractAddr)
		if len(code) != 0 {
			t.Error("deposit contract should NOT be deployed at block 101")
		}
	})
}
