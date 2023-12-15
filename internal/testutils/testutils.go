package testutils

import (
	"math/big"
	"testing"
	"time"
)

// DefaultWaitTimeout is the default wait timeout. If you have a *testing.T, use WaitTimeout instead.
const DefaultWaitTimeout = 30 * time.Second

// SimulatedChainID is the chain ID for the go-ethereum simulated backend
var SimulatedChainID = big.NewInt(1337)

// SkipShort skips tb during -short runs, and notes why.
func SkipShort(tb testing.TB, why string) {
	if testing.Short() {
		tb.Skipf("skipping: %s", why)
	}
}
