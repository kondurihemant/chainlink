package transaction

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gopkg.in/guregu/null.v4"

	"github.com/smartcontractkit/chainlink-common/pkg/logger"
	"github.com/smartcontractkit/chainlink/v2/common/client"
	feetypes "github.com/smartcontractkit/chainlink/v2/common/fee/types"
	"github.com/smartcontractkit/chainlink/v2/common/txmgr"
	txmgrtypes "github.com/smartcontractkit/chainlink/v2/common/txmgr/types"
	"github.com/smartcontractkit/chainlink/v2/common/types"
)

var (
	ErrTxnNotFound  = fmt.Errorf("transaction not found")
	ErrTxnAbandoned = fmt.Errorf("transaction abandoned")
)

// TxStore ...
type TxStore[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
] interface {
	Create(tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) error
	Update(tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) error
	Delete(tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) error
}

type Broadcaster[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
] interface {
	ProcessTransaction(context.Context, *txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) (client.SendTxReturnCode, error)
}

type BroadcasterListenerConfig interface {
	FallbackPollInterval() time.Duration
}

// StateManager is the state of a given from address
type StateManager[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
] struct {
	lggr logger.Logger

	listenerConfig BroadcasterListenerConfig

	lock               sync.RWMutex
	idempotencyKeyToTx map[string]*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]
	unstartedQueue     *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]
	inprogress         *txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]
	// TODO(jtw): using the TX ID as the key for the map might not make sense since the ID is set by the
	// postgres DB which creates a dependency on the postgres DB. We should consider creating a UUID or ULID
	// TX ID -> TX
	unconfirmed map[int64]*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]

	// availableBroadcasterSpots is a semaphore for the number of transactions that are in the in_progress state
	availableBroadcasterSpots chan struct{}
	broadcasterTriggerCh      chan struct{}
	Broadcaster               Broadcaster[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]

	// inflightTransactionSpots is a semaphore for the number of transactions that are in the unconfirmed state
	inflightTransactionSpots chan struct{}
	maxInFlightTransactions  int

	TxStore TxStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]
}

// NewStateManager returns a new StateManager instance
func NewStateManager[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
](lggr logger.Logger, maxUnstarted, maxUnconfirmed int) *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE] {
	as := StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{
		lggr: lggr,

		idempotencyKeyToTx:        map[string]*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{},
		unstartedQueue:            NewPriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE](maxUnstarted),
		inprogress:                nil,
		unconfirmed:               map[int64]*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{},
		availableBroadcasterSpots: make(chan struct{}, 1),
		inflightTransactionSpots:  make(chan struct{}, maxUnconfirmed),

		TxStore: NewInMemoryFileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE](), // TODO(jtw): REPLACE WITH SOMETHING ELSE
	}

	return &as
}

// NO RETRY WHEN
// - NO TXS IN UNSTARTED QUEUE
// - CONTEXT CANCELLED ERROR
// - IF txType is legacy but fee is not legacy
// - if txType is dynamic but

// RETRY WHEN
// - ERROR PERSISTING TX
// - ERROR WITH GETTING NEXT SEQUENCE
// - error getting fee from fee estimator
// - error bumping fee from fee estimator
// - error creating new legacy txAttempt
// - TX already in progress

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) UpdateNextUnstartedTxToInProgress(
	ctx context.Context,
	nextSequence func(context.Context, ADDR) (SEQ, error),
	newTxAttempt func(ctx context.Context, tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], lggr logger.Logger, opts ...feetypes.Opt) (
		attempt txmgrtypes.TxAttempt[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], fee FEE, feeLimit uint32, retryable bool, err error),
) (tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], retyable bool, err error) {
	if err := as.takeAvailableInflightTransactionSpot(ctx); err != nil {
		return tx, false, err
	}

	as.lock.Lock()
	defer as.lock.Unlock()

	if as.inprogress != nil {
		as.releaseInflightTransactionSpot()
		return tx, true, fmt.Errorf("a transaction is already in progress")
	}

	etx := as.unstartedQueue.RemoveNextTx()
	if etx == nil {
		as.releaseInflightTransactionSpot()
		return tx, false, fmt.Errorf("no unstarted transaction to move to in_progress: %w", ErrTxnNotFound)
	}

	sequence, err := nextSequence(ctx, etx.FromAddress)
	if err != nil {
		return tx, true, err
	}
	etx.Sequence = &sequence
	etx.State = txmgr.TxInProgress
	txAttempt, _, _, retryable, err := newTxAttempt(ctx, *etx, as.lggr)
	if err != nil {
		etx.State = txmgr.TxUnstarted
		etx.Sequence = nil
		as.unstartedQueue.AddTx(etx)
		as.releaseInflightTransactionSpot()

		return tx, retryable, fmt.Errorf("failed on NewAttempt: %w", err)
	}
	etx.TxAttempts = append(etx.TxAttempts, txAttempt)

	// PERSIST TX
	if err := as.TxStore.Update(*etx); err != nil {
		etx.State = txmgr.TxUnstarted
		etx.TxAttempts = etx.TxAttempts[:len(etx.TxAttempts)-1]
		etx.Sequence = nil
		as.unstartedQueue.AddTx(etx)
		as.releaseInflightTransactionSpot()

		return tx, true, fmt.Errorf("failed to persist update of transaction from unstarted to inprogress: %w", err)
	}
	as.inprogress = etx

	return *etx, false, nil
}

// FindTxWithIdempotencyKey returns the transaction with the given idempotency key.
// If no transaction is found, it returns an error.
func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) FindTxWithIdempotencyKey(key string) (txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], error) {
	as.lock.RLock()
	defer as.lock.RUnlock()

	tx, ok := as.idempotencyKeyToTx[key]
	if !ok {
		return txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{}, fmt.Errorf("find_tx_with_idempotency_key: %w", ErrTxnNotFound)
	}

	return *tx, nil
}

// IsUnstartedQueueFull returns true if the unstarted queue is full.
func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) IsUnstartedQueueFull() bool {
	return as.unstartedQueue.Len() >= as.unstartedQueue.Cap()
}

// EnqueueUnstartedTransaction takes a transaction and adds it to the unstarted queue.
// If the queue is full, it returns an error.
// If the transaction is able to be added to the queue and it is successfully persisted, it is added to the unstarted queue and returned.
func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) EnqueueUnstartedTransaction(tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) (txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], error) {
	as.lock.Lock()
	defer as.lock.Unlock()

	// CHECK IF CAPACITY HAS BEEN REACHED
	if as.IsUnstartedQueueFull() {
		return txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{}, fmt.Errorf("move_tx_to_unstarted: unstarted queue capacity has been reached")
	}

	tx.State = txmgr.TxUnstarted

	// PERSIST TX
	if err := as.TxStore.Create(tx); err != nil {
		return txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{}, fmt.Errorf("failed to persist transaction: %w", err)
	}

	as.unstartedQueue.AddTx(&tx)
	if tx.IdempotencyKey != nil {
		as.idempotencyKeyToTx[*tx.IdempotencyKey] = &tx
	}

	return tx, nil
}

// GetTxInProgess returns the transaction that is currently in progress.
// If no transaction is in progress, it returns an error.
func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) GetTxInProgress() (txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], error) {
	as.lock.RLock()
	defer as.lock.RUnlock()

	if as.inprogress == nil {
		return txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{}, fmt.Errorf("get_tx_in_progress: %w", ErrTxnNotFound)
	}

	return *as.inprogress, nil
}

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) takeAvailableInflightTransactionSpot(ctx context.Context) error {
	if as.maxInFlightTransactions <= 0 {
		return nil
	}

	select {
	case as.inflightTransactionSpots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) releaseInflightTransactionSpot() {
	if as.maxInFlightTransactions <= 0 {
		return
	}

	select {
	case <-as.inflightTransactionSpots:
		// Spot freed successfully
	default:
	}
}

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Close() {
	as.lock.Lock()
	defer as.lock.Unlock()

	as.unstartedQueue.Close()
	as.unstartedQueue = nil
	as.inprogress = nil
	clear(as.unconfirmed)
	clear(as.idempotencyKeyToTx)
}

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) unstartedCount() int {
	as.lock.RLock()
	defer as.lock.RUnlock()

	return as.unstartedQueue.Len()
}
func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) unconfirmedCount() int {
	as.lock.RLock()
	defer as.lock.RUnlock()

	return len(as.unconfirmed)
}

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) peekNextUnstartedTx() (*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], error) {
	as.lock.RLock()
	defer as.lock.RUnlock()

	tx := as.unstartedQueue.PeekNextTx()
	if tx == nil {
		return nil, fmt.Errorf("peek_next_unstarted_tx: %w", ErrTxnNotFound)
	}

	return tx, nil
}

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) peekInProgressTx() (*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], error) {
	as.lock.RLock()
	defer as.lock.RUnlock()

	tx := as.inprogress
	if tx == nil {
		return nil, fmt.Errorf("peek_in_progress_tx: %w", ErrTxnNotFound)
	}

	return tx, nil
}

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) moveInProgressToUnconfirmed(txAttempt txmgrtypes.TxAttempt[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) error {
	as.lock.Lock()
	defer as.lock.Unlock()

	tx := as.inprogress
	if tx == nil {
		return fmt.Errorf("move_in_progress_to_unconfirmed: no transaction in progress")
	}
	tx.State = txmgr.TxUnconfirmed

	var found bool
	for i := 0; i < len(tx.TxAttempts); i++ {
		if tx.TxAttempts[i].ID == txAttempt.ID {
			tx.TxAttempts[i] = txAttempt
			found = true
		}
	}
	if !found {
		// NOTE(jtw): this would mean that the TxAttempt did not exist for the Tx
		// NOTE(jtw): should this log a warning?
		// NOTE(jtw): can this happen?
		tx.TxAttempts = append(tx.TxAttempts, txAttempt)
	}

	as.unconfirmed[tx.ID] = tx
	as.inprogress = nil

	return nil
}

func (as *StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) abandon() {
	as.lock.Lock()
	defer as.lock.Unlock()

	for as.unstartedQueue.Len() > 0 {
		tx := as.unstartedQueue.RemoveNextTx()
		as.abandonTx(tx)
	}

	if as.inprogress != nil {
		tx := as.inprogress
		as.abandonTx(tx)
		as.inprogress = nil
	}
	for _, tx := range as.unconfirmed {
		as.abandonTx(tx)
	}
	for _, tx := range as.idempotencyKeyToTx {
		as.abandonTx(tx)
	}

	clear(as.unconfirmed)
}

func (*StateManager[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) abandonTx(tx *txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) {
	if tx == nil {
		return
	}

	tx.State = txmgr.TxFatalError
	tx.Sequence = nil
	tx.Error = null.NewString("abandoned", true)
}
