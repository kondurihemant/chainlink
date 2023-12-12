package txmgr

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smartcontractkit/chainlink-common/pkg/logger"
	feetypes "github.com/smartcontractkit/chainlink/v2/common/fee/types"
	txmgrtypes "github.com/smartcontractkit/chainlink/v2/common/txmgr/types"
	"github.com/smartcontractkit/chainlink/v2/common/types"
	"github.com/smartcontractkit/chainlink/v2/common/v2/x/transaction"
)

var (
	ErrAddressNotFound = errors.New("address not found")
)

// AddressStateManager ...
type AddressStateManager[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
] struct {
	lggr logger.Logger

	chainID                      CHAIN_ID
	maxUnconfirmed, maxUnstarted int

	txConfig txmgrtypes.TransactionManagerTransactionsConfig
	fwdMgr   txmgrtypes.ForwarderManager[ADDR]
	keyStore txmgrtypes.KeyStore[ADDR, CHAIN_ID, SEQ]

	sync.RWMutex
	AddressStateTxManagers map[ADDR]*transaction.StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]
}

// NewAddressStateManager ...
func NewAddressStateManager[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
](
	lggr logger.Logger,
	chainID CHAIN_ID,
	keyStore txmgrtypes.KeyStore[ADDR, CHAIN_ID, SEQ],
	txConfig txmgrtypes.TransactionManagerTransactionsConfig,
	fwdMgr txmgrtypes.ForwarderManager[ADDR],
) *AddressStateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE] {
	asm := &AddressStateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{
		lggr:     lggr,
		chainID:  chainID,
		txConfig: txConfig,
		fwdMgr:   fwdMgr,
		keyStore: keyStore,

		AddressStateTxManagers: map[ADDR]*transaction.StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{},

		// TODO: Make these configurable
		maxUnconfirmed: 100,
		maxUnstarted:   50,
	}

	return asm
}

func (asm *AddressStateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Start(_ context.Context) error {
	asm.Lock()
	defer asm.Unlock()

	addresses, err := asm.keyStore.EnabledAddressesForChain(asm.chainID)
	if err != nil {
		return fmt.Errorf("new_in_memory_store: %w", err)
	}

	for _, address := range addresses {
		asm.AddressStateTxManagers[address] = transaction.NewStateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE](asm.lggr, asm.maxUnstarted, asm.maxUnconfirmed)
	}

	return nil
}

func (asm *AddressStateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Close() error {
	asm.Lock()
	defer asm.Unlock()

	for _, as := range asm.AddressStateTxManagers {
		as.Close()
	}

	return nil
}

// CreateTransaction ...
func (asm *AddressStateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) CreateTransaction(txRequest txmgrtypes.TxRequest[ADDR, TX_HASH]) (txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], error) {
	asm.Lock()
	defer asm.Unlock()

	// GET ADDRESS TRANSACTION STATE MANAGER
	tm, ok := asm.AddressStateTxManagers[txRequest.FromAddress]
	if !ok {
		return txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{}, ErrAddressNotFound
	}

	// CHECK IDEMPOTENCY KEY
	// Check for existing Tx with IdempotencyKey. If found, return the Tx and do nothing
	// Skipping CreateTransaction to avoid double send
	if txRequest.IdempotencyKey != nil {
		tx, err := tm.FindTxWithIdempotencyKey(*txRequest.IdempotencyKey)
		if err != nil && !errors.Is(err, transaction.ErrTxnNotFound) {
			return txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{}, fmt.Errorf("failed to search for transaction with IdempotencyKey: %w", err)

		}
		if err == nil {
			asm.lggr.Infow("Found a Tx with IdempotencyKey. Returning existing Tx without creating a new one.", "IdempotencyKey", *txRequest.IdempotencyKey)
			return tx, nil
		}
	}

	// TODO: DO WE NEED TO CHECK FOR THE PIPELINE TASK RUN ID?
	// TODO: DO WE NEED PRUNING STRATEGIES STILL?

	// Initialize Transaction
	tx := txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{
		UUID:              uuid.New(),
		CreatedAtUnixNano: time.Now().UnixNano(),

		IdempotencyKey: txRequest.IdempotencyKey,
		FromAddress:    txRequest.FromAddress,
		ToAddress:      txRequest.ToAddress,
		Value:          txRequest.Value,
		EncodedPayload: txRequest.EncodedPayload,
		FeeLimit:       txRequest.FeeLimit,
		NewMeta:        txRequest.Meta,
		ChainID:        asm.chainID,
		//MinConfirmations: txRequest.MinConfirmations,
		NewPipelineTaskRunID: txRequest.PipelineTaskRunID,
		NewTransmitChecker:   txRequest.Checker,
		SignalCallback:       txRequest.SignalCallback,

		TxAttempts: []txmgrtypes.TxAttempt[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{},
	}

	return tm.EnqueueUnstartedTransaction(tx)
}

func (asm *AddressStateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) UpdateNextUnstartedTxToInProgress(
	ctx context.Context,
	fromAddress ADDR,
	nextSequence func(context.Context, ADDR) (SEQ, error),
	newTxAttempt func(ctx context.Context, tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], lggr logger.Logger, opts ...feetypes.Opt) (
		attempt txmgrtypes.TxAttempt[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], fee FEE, feeLimit uint32, retryable bool, err error),
) (txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], bool /* retryable */, error) {
	asm.RLock()
	defer asm.RUnlock()

	// GET ADDRESS TRANSACTION STATE MANAGER
	tm, ok := asm.AddressStateTxManagers[fromAddress]
	if !ok {
		return txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{}, false, ErrAddressNotFound
	}

	return tm.UpdateNextUnstartedTxToInProgress(ctx, nextSequence, newTxAttempt)
}

func (asm *AddressStateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) GetTxInProgress(fromAddress ADDR) (txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], error) {
	asm.RLock()
	defer asm.RUnlock()

	// GET ADDRESS TRANSACTION STATE MANAGER
	tm, ok := asm.AddressStateTxManagers[fromAddress]
	if !ok {
		return txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{}, ErrAddressNotFound
	}

	return tm.GetTxInProgress()
}
