package vm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

func TestPulseChainFlexChainConfig(t *testing.T) {
	config := &params.ChainConfig{
		ChainID:              big.NewInt(369),
		PrimordialPulseBlock: big.NewInt(100),
		HomesteadBlock:       big.NewInt(0),
		LondonBlock:          big.NewInt(0),
	}

	tests := []struct {
		name            string
		blockNumber     *big.Int
		expectedChainID int64
	}{
		{"before PrimordialPulseBlock", big.NewInt(50), 1},
		{"at PrimordialPulseBlock", big.NewInt(100), 369},
		{"after PrimordialPulseBlock", big.NewInt(150), 369},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockCtx := BlockContext{
				BlockNumber: tt.blockNumber,
			}
			evm := NewEVM(blockCtx, nil, config, Config{})
			got := evm.ChainConfig().ChainID.Int64()
			if got != tt.expectedChainID {
				t.Errorf("EVM ChainConfig().ChainID at block %s: got %d, want %d",
					tt.blockNumber, got, tt.expectedChainID)
			}
		})
	}

	// Verify original config is not mutated
	t.Run("original config not mutated", func(t *testing.T) {
		blockCtx := BlockContext{BlockNumber: big.NewInt(50)} // pre-fork
		_ = NewEVM(blockCtx, nil, config, Config{})
		if config.ChainID.Int64() != 369 {
			t.Errorf("original config mutated: got ChainID %d, want 369", config.ChainID.Int64())
		}
	})
}
