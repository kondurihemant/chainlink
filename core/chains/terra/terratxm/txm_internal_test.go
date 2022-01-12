package terratxm

import (
	"testing"
	"time"

	"github.com/pkg/errors"

	tmservicetypes "github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	cosmostypes "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	tcmocks "github.com/smartcontractkit/chainlink-terra/pkg/terra/client/mocks"
	"github.com/smartcontractkit/chainlink/core/internal/testutils/pgtest"
	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/chainlink/core/services/keystore"
	"github.com/smartcontractkit/chainlink/core/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
	wasmtypes "github.com/terra-money/core/x/wasm/types"
)

func TestErrMatch(t *testing.T) {
	errStr := "rpc error: code = InvalidArgument desc = failed to execute message; message index: 0: Error parsing into type my_first_contract::msg::ExecuteMsg: unknown variant `blah`, expected `increment` or `reset`: execute wasm contract failed: invalid request"
	m := failedMsgIndexRe.FindStringSubmatch(errStr)
	require.Equal(t, 2, len(m))
	assert.Equal(t, m[1], "0")
}

func generateExecuteMsg(t *testing.T, from, to cosmostypes.AccAddress) []byte {
	msg1 := wasmtypes.NewMsgExecuteContract(from, to, []byte(`{"transmit":{"report_context":"","signatures":[""],"report":""}}`), cosmostypes.Coins{})
	d, err := msg1.Marshal()
	require.NoError(t, err)
	return d
}

func TestTxm(t *testing.T) {
	db := pgtest.NewSqlxDB(t)
	lggr := logger.TestLogger(t)
	ks := keystore.New(db, utils.FastScryptParams, lggr, pgtest.NewPGCfg(true))
	require.NoError(t, ks.Unlock("blah"))
	k1, err := ks.Terra().Create()
	require.NoError(t, err)
	sender1, err := cosmostypes.AccAddressFromBech32(k1.PublicKeyStr())
	require.NoError(t, err)
	k2, err := ks.Terra().Create()
	require.NoError(t, err)
	sender2, err := cosmostypes.AccAddressFromBech32(k2.PublicKeyStr())
	require.NoError(t, err)
	contract, err := cosmostypes.AccAddressFromBech32("terra1pp76d50yv2ldaahsdxdv8mmzqfjr2ax97gmue8")
	require.NoError(t, err)

	t.Run("single msg", func(t *testing.T) {
		tc := new(tcmocks.ReaderWriter)
		tc.On("Account", mock.Anything).Return(uint64(0), uint64(0), nil)
		tc.On("GasPrice").Return(cosmostypes.NewDecCoinFromDec("uluna", cosmostypes.MustNewDecFromStr("0.01")))
		tc.On("SimulateUnsigned", mock.Anything, mock.Anything).Return(&txtypes.SimulateResponse{GasInfo: &cosmostypes.GasInfo{
			GasUsed: 1_000_000,
		}}, nil)
		tc.On("LatestBlock").Return(&tmservicetypes.GetLatestBlockResponse{Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: 1},
		}}, nil)
		tc.On("CreateAndSign", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]byte{0x01}, nil)
		tc.On("Broadcast", mock.Anything, mock.Anything).Return(&txtypes.BroadcastTxResponse{
			TxResponse: &cosmostypes.TxResponse{TxHash: "0x123"},
		}, nil)
		tc.On("Tx", mock.Anything).Return(&txtypes.GetTxResponse{
			Tx:         &txtypes.Tx{},
			TxResponse: &cosmostypes.TxResponse{TxHash: "0x123"},
		}, nil)

		txm := NewTxm(db, tc, ks.Terra(), lggr, pgtest.NewPGCfg(true), nil, time.Second)

		// Enqueue a single msg, then send it in a batch
		id1, err := txm.Enqueue(contract.String(), generateExecuteMsg(t, sender1, contract))
		require.NoError(t, err)
		txm.sendMsgBatch()

		// Should be in completed state
		completed, err := txm.orm.SelectMsgsWithIDs([]int64{id1})
		require.NoError(t, err)
		require.Equal(t, 1, len(completed))
		assert.Equal(t, completed[0].State, Confirmed)
		tc.AssertExpectations(t)
	})

	t.Run("two msgs different accounts", func(t *testing.T) {
		tc := new(tcmocks.ReaderWriter)
		tc.On("Account", mock.Anything).Return(uint64(0), uint64(0), nil)
		tc.On("GasPrice").Return(cosmostypes.NewDecCoinFromDec("uluna", cosmostypes.MustNewDecFromStr("0.01")))
		tc.On("SimulateUnsigned", mock.Anything, mock.Anything).Return(&txtypes.SimulateResponse{GasInfo: &cosmostypes.GasInfo{
			GasUsed: 1_000_000,
		}}, nil)
		tc.On("LatestBlock").Return(&tmservicetypes.GetLatestBlockResponse{Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: 1},
		}}, nil)
		tc.On("CreateAndSign", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]byte{0x01}, nil)
		tc.On("Broadcast", mock.Anything, mock.Anything).Return(&txtypes.BroadcastTxResponse{
			TxResponse: &cosmostypes.TxResponse{TxHash: "0x123"},
		}, nil)
		tc.On("Tx", mock.Anything).Return(&txtypes.GetTxResponse{
			Tx:         &txtypes.Tx{},
			TxResponse: &cosmostypes.TxResponse{TxHash: "0x123"},
		}, nil)

		txm := NewTxm(db, tc, ks.Terra(), lggr, pgtest.NewPGCfg(true), nil, time.Second)

		id1, err := txm.Enqueue(contract.String(), generateExecuteMsg(t, sender1, contract))
		require.NoError(t, err)
		id2, err := txm.Enqueue(contract.String(), generateExecuteMsg(t, sender2, contract))
		require.NoError(t, err)
		txm.sendMsgBatch()

		// Should be in completed state
		completed, err := txm.orm.SelectMsgsWithIDs([]int64{id1, id2})
		require.NoError(t, err)
		require.Equal(t, 2, len(completed))
		assert.Equal(t, completed[0].State, Confirmed)
		assert.Equal(t, completed[1].State, Confirmed)
		tc.AssertExpectations(t)
	})

	t.Run("sim single failure single msg", func(t *testing.T) {
		tc := new(tcmocks.ReaderWriter)
		tc.On("SimulateUnsigned", mock.Anything, mock.Anything).Return(&txtypes.SimulateResponse{GasInfo: &cosmostypes.GasInfo{
			GasUsed: 1_000_000,
		}}, errors.New("failed to execute message; message index: 0:")).Once()
		txm := NewTxm(db, tc, ks.Terra(), lggr, pgtest.NewPGCfg(true), nil, time.Second)
		sr, err := txm.simulate([]TerraMsg{{ID: 1}}, 0)
		require.NoError(t, err)
		require.Equal(t, 1, len(sr.failed))
		require.Equal(t, 0, len(sr.succeeded))
		require.Equal(t, uint64(0), sr.gasLimit)
		tc.AssertExpectations(t)
	})

	t.Run("sim single failure multiple msgs", func(t *testing.T) {
		tc := new(tcmocks.ReaderWriter)
		tc.On("SimulateUnsigned", mock.Anything, mock.Anything).Return(&txtypes.SimulateResponse{GasInfo: &cosmostypes.GasInfo{
			GasUsed: 1_000,
		}}, errors.New("failed to execute message; message index: 1:")).Once()
		// Should simulate one more time
		tc.On("SimulateUnsigned", mock.Anything, mock.Anything).Return(&txtypes.SimulateResponse{GasInfo: &cosmostypes.GasInfo{
			GasUsed: 1_000_000,
		}}, nil).Once()
		txm := NewTxm(db, tc, ks.Terra(), lggr, pgtest.NewPGCfg(true), nil, time.Second)
		sr, err := txm.simulate([]TerraMsg{{ID: 1}, {ID: 2}}, 0)
		require.NoError(t, err)
		require.Equal(t, 1, len(sr.failed))
		require.Equal(t, 1, len(sr.succeeded))
		require.Equal(t, uint64(1_000_000), sr.gasLimit)
		tc.AssertExpectations(t)
	})

	t.Run("sim all failed", func(t *testing.T) {
		tc := new(tcmocks.ReaderWriter)
		tc.On("SimulateUnsigned", mock.Anything, mock.Anything).Return(&txtypes.SimulateResponse{GasInfo: &cosmostypes.GasInfo{
			GasUsed: 1_000,
		}}, errors.New("failed to execute message; message index: 0:")).Times(3)
		txm := NewTxm(db, tc, ks.Terra(), lggr, pgtest.NewPGCfg(true), nil, time.Second)
		sr, err := txm.simulate([]TerraMsg{{ID: 1}, {ID: 2}, {ID: 3}}, 0)
		require.NoError(t, err)
		require.Equal(t, 3, len(sr.failed))
		require.Equal(t, 0, len(sr.succeeded))
		require.Equal(t, uint64(0), sr.gasLimit)
		tc.AssertExpectations(t)
	})

	t.Run("sim all succeed", func(t *testing.T) {
		tc := new(tcmocks.ReaderWriter)
		tc.On("SimulateUnsigned", mock.Anything, mock.Anything).Return(&txtypes.SimulateResponse{GasInfo: &cosmostypes.GasInfo{
			GasUsed: 1_000,
		}}, nil).Twice()
		txm := NewTxm(db, tc, ks.Terra(), lggr, pgtest.NewPGCfg(true), nil, time.Second)
		sr, err := txm.simulate([]TerraMsg{{ID: 1}, {ID: 2}, {ID: 3}}, 0)
		require.NoError(t, err)
		require.Equal(t, 0, len(sr.failed))
		require.Equal(t, 3, len(sr.succeeded))
		require.Equal(t, uint64(1_000), sr.gasLimit)
		tc.AssertExpectations(t)
	})

	t.Run("failed to confirm", func(t *testing.T) {
		tc := new(tcmocks.ReaderWriter)
		tc.On("Tx", mock.Anything).Return(&txtypes.GetTxResponse{
			Tx:         &txtypes.Tx{},
			TxResponse: &cosmostypes.TxResponse{TxHash: "0x123"},
		}, errors.New("not found")).Twice()
		txm := NewTxm(db, tc, ks.Terra(), lggr, pgtest.NewPGCfg(true), nil, time.Second)
		txm.confirmPollPeriod = 0 * time.Second
		txm.confirmMaxPolls = 2
		i, err := txm.orm.InsertMsg("blah", []byte{0x01})
		require.NoError(t, err)
		err = txm.confirmTx("0x123", []TerraMsg{{ID: i}})
		require.NoError(t, err)
		m, err := txm.orm.SelectMsgsWithIDs([]int64{i})
		require.NoError(t, err)
		require.Equal(t, 1, len(m))
		assert.Equal(t, Errored, m[0].State)
		tc.AssertExpectations(t)
	})
}
