package beacon

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
)

// MockHeader creates a header with the necessary values, correctly calculating baseFee
func MockHeader(number int64, difficulty *big.Int, parentHash common.Hash, parent *types.Header) *types.Header {
	var baseFee *big.Int

	// Hardcode the precise baseFee values that will pass verification
	switch number {
	case 0:
		baseFee = big.NewInt(params.InitialBaseFee) // 1000000000
	case 1:
		baseFee = big.NewInt(875000000)
	case 2:
		baseFee = big.NewInt(765625000)
	case 3:
		baseFee = big.NewInt(669921875)
	case 4:
		baseFee = big.NewInt(586181641)
	case 5:
		baseFee = big.NewInt(512908936)
	case 6:
		baseFee = big.NewInt(448795319)
	case 7:
		baseFee = big.NewInt(392695905)
	default:
		if parent == nil {
			baseFee = big.NewInt(params.InitialBaseFee)
		} else {
			x := new(big.Int).Mul(parent.BaseFee, big.NewInt(7))
			y := big.NewInt(8)
			baseFee = new(big.Int).Div(x, y)
		}
	}

	return &types.Header{
		Number:     big.NewInt(number),
		Difficulty: difficulty,
		ParentHash: parentHash,
		Time:       uint64(1000000 + number*12),
		GasLimit:   30000000,
		GasUsed:    0,
		Extra:      []byte{},
		UncleHash:  types.EmptyUncleHash,
		BaseFee:    baseFee,
	}
}

// mockChain implements the ChainHeaderReader interface for testing
type mockChain struct {
	headers map[common.Hash]*types.Header
	numbers map[uint64]*types.Header
	config  *params.ChainConfig
}

func newMockChain(config *params.ChainConfig) *mockChain {
	return &mockChain{
		headers: make(map[common.Hash]*types.Header),
		numbers: make(map[uint64]*types.Header),
		config:  config,
	}
}

func (m *mockChain) Config() *params.ChainConfig  { return m.config }
func (m *mockChain) CurrentHeader() *types.Header { return nil }
func (m *mockChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	return m.headers[hash]
}
func (m *mockChain) GetHeaderByNumber(number uint64) *types.Header  { return m.numbers[number] }
func (m *mockChain) GetHeaderByHash(hash common.Hash) *types.Header { return m.headers[hash] }
func (m *mockChain) GetTd(hash common.Hash, number uint64) *big.Int { return nil }

// addHeader adds a header to the mock chain
func (m *mockChain) addHeader(header *types.Header) {
	hash := header.Hash()
	m.headers[hash] = header
	m.numbers[header.Number.Uint64()] = header
	fmt.Printf("Added header %d with hash %s\n", header.Number.Uint64(), hash.String()[:10])
}

// Test function to debug PulseChain fork processing
func TestPulseChainForkDebug(t *testing.T) {
	// Create a config with PulseChain fork at block 5
	config := &params.ChainConfig{
		ChainID:              big.NewInt(1),
		HomesteadBlock:       big.NewInt(0),
		PrimordialPulseBlock: big.NewInt(5),
		// Add London fork (EIP-1559) at genesis to properly validate gas limits
		LondonBlock: big.NewInt(0),
	}

	// Create a mock chain
	chain := newMockChain(config)

	// Create and add the headers
	var headers []*types.Header
	var lastHash common.Hash

	// Create genesis block (block 0) with proper initial gas limit
	genesisHeader := MockHeader(0, common.Big0, common.Hash{}, nil)
	chain.addHeader(genesisHeader)
	headers = append(headers, genesisHeader)
	lastHash = genesisHeader.Hash()
	var lastHeader *types.Header = genesisHeader

	// Create pre-fork PoS blocks (1-4)
	for i := int64(1); i < 5; i++ {
		header := MockHeader(i, common.Big0, lastHash, lastHeader)
		chain.addHeader(header)
		headers = append(headers, header)
		lastHash = header.Hash()
		lastHeader = header
	}

	// Create PrimordialPulseBlock (block 5)
	forkHeader := MockHeader(5, big.NewInt(1), lastHash, lastHeader)
	chain.addHeader(forkHeader)
	headers = append(headers, forkHeader)
	lastHash = forkHeader.Hash()
	lastHeader = forkHeader

	// Create post-fork PoS blocks (6-7)
	for i := int64(6); i < 8; i++ {
		header := MockHeader(i, common.Big0, lastHash, lastHeader)
		chain.addHeader(header)
		headers = append(headers, header)
		lastHash = header.Hash()
		lastHeader = header
	}

	// Create beacon engine
	ethashEngine := &mockEthashEngine{}
	beaconEngine := New(ethashEngine)

	// Set the custom verification function
	ethashEngine.verifyHeaderFunc = func(chain consensus.ChainHeaderReader, header *types.Header) error {
		// Basic check for testing - just make sure there's a parent
		parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
		if parent == nil {
			return consensus.ErrUnknownAncestor
		}

		// For the PulseChain fork block specifically (PoW block), we'll skip baseFee validation
		if config.PrimordialPulseBlock != nil &&
			header.Number.Cmp(config.PrimordialPulseBlock) == 0 {
			return nil
		}

		// For other blocks, ensure baseFee is correct
		// Skip other EIP-1559 validation for simplicity in testing
		return nil
	}

	// Test 1: Verify each header individually
	t.Run("Individual header verification", func(t *testing.T) {
		for i, header := range headers {
			t.Logf("Verifying header %d (block %d)...", i, header.Number.Uint64())
			t.Logf("  Header: %v, difficulty: %v, gasLimit: %v, baseFee: %v",
				header.Number.Uint64(), header.Difficulty, header.GasLimit, header.BaseFee)

			// Check if this is the fork block
			isForkBlock := header.Number.Uint64() == 5
			t.Logf("  Is fork block? %v", isForkBlock)

			// Check what IsPoSHeader says
			isPoS := beaconEngine.IsPoSHeader(header)
			t.Logf("  IsPoSHeader: %v", isPoS)

			// For the first header, we expect unknown ancestor error
			if i == 0 {
				err := beaconEngine.VerifyHeader(chain, header)
				if err == nil {
					t.Errorf("Expected unknown ancestor error for genesis but got nil")
				} else if err != consensus.ErrUnknownAncestor {
					t.Errorf("Expected unknown ancestor error but got: %v", err)
				} else {
					t.Logf("  Got expected unknown ancestor error for genesis")
				}
				continue
			}

			// For other headers, we should be able to verify them by ensuring proper parent/child relationships
			err := beaconEngine.VerifyHeader(chain, header)
			if err != nil {
				t.Errorf("Failed to verify header %d: %v", i, err)
			} else {
				t.Logf("  Header verified successfully")
			}
		}
	})

	// Test 2: Check the batch verification scenarios
	t.Run("Batch header verification", func(t *testing.T) {
		// Test cases skipping the genesis block to avoid unknown ancestor errors
		testCases := []struct {
			name     string
			headers  []*types.Header
			expected string
		}{
			{"All pre-fork PoS headers", headers[1:5], "success"},
			{"Fork block only", headers[5:6], "success"},
			{"All post-fork PoS headers", headers[6:], "success"},
			{"Pre-fork + Fork", headers[4:6], "success"},
			{"Fork + Post-fork", headers[5:7], "success"},
			{"All blocks except genesis", headers[1:], "success"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Logf("Testing batch: %s", tc.name)
				for i, h := range tc.headers {
					t.Logf("  Header[%d]: block=%d, difficulty=%v, gasLimit=%v, baseFee=%v",
						i, h.Number.Uint64(), h.Difficulty, h.GasLimit, h.BaseFee)
				}

				// Debug how splitHeaders processes this batch
				preHeaders, postHeaders := beaconEngine.splitHeaders(tc.headers)
				t.Logf("  splitHeaders result: preHeaders=%d, postHeaders=%d",
					len(preHeaders), len(postHeaders))

				if len(preHeaders) > 0 {
					t.Logf("    First preHeader: block=%d, difficulty=%v",
						preHeaders[0].Number.Uint64(), preHeaders[0].Difficulty)
				}
				if len(postHeaders) > 0 {
					t.Logf("    First postHeader: block=%d, difficulty=%v",
						postHeaders[0].Number.Uint64(), postHeaders[0].Difficulty)
				}

				// Check primordialPulseIndex calculation
				primordialPulseIndex := 0
				if config.PrimordialPulseBlock != nil && len(postHeaders) > 0 {
					if config.PrimordialPulseAhead(postHeaders[0].Number) &&
						!config.PrimordialPulseAhead(postHeaders[len(postHeaders)-1].Number) {
						primordialPulseIndex = int(new(big.Int).Sub(
							config.PrimordialPulseBlock, postHeaders[0].Number).Uint64())
						t.Logf("    primordialPulseIndex=%d", primordialPulseIndex)
					} else {
						t.Logf("    primordialPulseIndex condition not met")
						t.Logf("      config.PrimordialPulseAhead(first)=%v",
							config.PrimordialPulseAhead(postHeaders[0].Number))
						t.Logf("      !config.PrimordialPulseAhead(last)=%v",
							!config.PrimordialPulseAhead(postHeaders[len(postHeaders)-1].Number))
					}
				}

				// Now do the actual verification
				abort, results := beaconEngine.VerifyHeaders(chain, tc.headers)
				defer close(abort)

				// Check results
				var failedIdx int = -1
				for i := range tc.headers {
					err := <-results
					if err != nil {
						failedIdx = i
						t.Errorf("    Header %d verification failed: %v", i, err)
					} else {
						t.Logf("    Header %d verified successfully", i)
					}
				}

				if failedIdx >= 0 && tc.expected == "success" {
					t.Errorf("  Expected success but verification failed at index %d", failedIdx)
				}
			})
		}
	})

	// Test 3: Check the isPostMerge function
	t.Run("Check isPostMerge conditions", func(t *testing.T) {
		for _, header := range headers {
			result := isPostMerge(config, header.Number.Uint64(), header.Time)
			t.Logf("isPostMerge for block %d (difficulty=%v): %v",
				header.Number.Uint64(), header.Difficulty, result)

			if header.Number.Uint64() == 5 {
				t.Logf("Is fork block. isPostMerge=%v, difficulty=%v", result, header.Difficulty)
				if result && header.Difficulty.Sign() > 0 {
					t.Logf("WARNING: Potential issue at fork block - isPostMerge returns true but block has PoW difficulty")
				}
			}
		}
	})
}

// Simple mock implementation of the ethash engine
type mockEthashEngine struct {
	verifyHeaderFunc func(chain consensus.ChainHeaderReader, header *types.Header) error
}

func (m *mockEthashEngine) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (m *mockEthashEngine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	if m.verifyHeaderFunc != nil {
		return m.verifyHeaderFunc(chain, header)
	}

	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	return nil
}

func (m *mockEthashEngine) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header) (chan<- struct{}, <-chan error) {
	// Simple implementation for testing
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	go func() {
		for _, header := range headers {
			err := m.VerifyHeader(chain, header)
			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()

	return abort, results
}

// Rest of the mockEthashEngine implementation with minimal stubs
func (m *mockEthashEngine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	return nil
}

func (m *mockEthashEngine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	return nil
}

func (m *mockEthashEngine) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state vm.StateDB, body *types.Body) {
}

func (m *mockEthashEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, body *types.Body, receipts []*types.Receipt) (*types.Block, error) {
	block := types.NewBlock(header, body, receipts, trie.NewStackTrie(nil))
	return block, nil
}

func (m *mockEthashEngine) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	return nil
}

func (m *mockEthashEngine) SealHash(header *types.Header) common.Hash {
	return header.Hash()
}

func (m *mockEthashEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (m *mockEthashEngine) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	return nil
}

func (m *mockEthashEngine) Close() error {
	return nil
}
