// Package pulse implements the PulseChain fork
package pulse

import (
	"math/big"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// Apply PrimordialPulse fork changes
func PrimordialPulseFork(state vm.StateDB, treasury *params.Treasury, chainID *big.Int) {
	applySacrificeCredits(state, treasury, chainID)
	replaceDepositContract(state)
}
