package configtest

import (
	"testing"

	"github.com/smartcontractkit/chainlink/v2/core/services/chainlink"
	"github.com/smartcontractkit/chainlink/v2/internal/testutils/configtest"
)

const DefaultPeerID = configtest.DefaultPeerID

// NewTestGeneralConfig returns a new chainlink.GeneralConfig with default test overrides and one chain with evmclient.NullClientChainID.
func NewTestGeneralConfig(t testing.TB) chainlink.GeneralConfig {
	return configtest.NewTestGeneralConfig(t)
}

// NewGeneralConfig returns a new chainlink.GeneralConfig with overrides.
// The default test overrides are applied before overrideFn, and include one chain with evmclient.NullClientChainID.
func NewGeneralConfig(t testing.TB, overrideFn func(*chainlink.Config, *chainlink.Secrets)) chainlink.GeneralConfig {
	return configtest.NewGeneralConfig(t, overrideFn)
}

// NewGeneralConfigSimulated returns a new chainlink.GeneralConfig with overrides, including the simulated EVM chain.
// The default test overrides are applied before overrideFn.
// The simulated chain (testutils.SimulatedChainID) replaces the null chain (evmclient.NullClientChainID).
func NewGeneralConfigSimulated(t testing.TB, overrideFn func(*chainlink.Config, *chainlink.Secrets)) chainlink.GeneralConfig {
	return configtest.NewGeneralConfigSimulated(t, overrideFn)
}
