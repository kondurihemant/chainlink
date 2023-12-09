package transaction

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jpillora/backoff"

	"github.com/smartcontractkit/chainlink/v2/common/client"
	"github.com/smartcontractkit/chainlink/v2/common/txmgr"
	"github.com/smartcontractkit/chainlink/v2/common/types"
	"github.com/smartcontractkit/chainlink/v2/core/logger"
)

var (
	ErrTxnNotFound  = fmt.Errorf("transaction not found")
	ErrTxnAbandoned = fmt.Errorf("transaction abandoned")
)

// TxStore ...
type TxStore[
	ADDR types.Hashable,
	SEQ types.Sequence,
] interface {
	Create(tx TX[ADDR, SEQ]) error
	Update(tx TX[ADDR, SEQ]) error
	Delete(tx TX[ADDR, SEQ]) error
}

type Broadcaster[
	ADDR types.Hashable,
	SEQ types.Sequence,
] interface {
	ProcessTransaction(context.Context, *TX[ADDR, SEQ]) (client.SendTxReturnCode, error)
}

type BroadcasterListenerConfig interface {
	FallbackPollInterval() time.Duration
}

// StateManager is the state of a given from address
type StateManager[
	ADDR types.Hashable,
	SEQ types.Sequence,
] struct {
	lggr logger.Logger

	listenerConfig BroadcasterListenerConfig

	lock               sync.RWMutex
	idempotencyKeyToTx map[string]*TX[ADDR, SEQ]
	unstartedQueue     *PriorityQueue[ADDR, SEQ]
	inprogress         *TX[ADDR, SEQ]
	// TODO(jtw): using the TX ID as the key for the map might not make sense since the ID is set by the
	// postgres DB which creates a dependency on the postgres DB. We should consider creating a UUID or ULID
	// TX ID -> TX
	unconfirmed map[uuid.UUID]*TX[ADDR, SEQ]

	// availableBroadcasterSpots is a semaphore for the number of transactions that are in the in_progress state
	availableBroadcasterSpots chan struct{}
	broadcasterTriggerCh      chan struct{}
	Broadcaster               Broadcaster[ADDR, SEQ]

	// inflightTransactionSpots is a semaphore for the number of transactions that are in the unconfirmed state
	inflightTransactionSpots chan struct{}

	TxStore TxStore[ADDR, SEQ]
}

// NewStateManager returns a new StateManager instance
func NewStateManager[
	ADDR types.Hashable,
	SEQ types.Sequence,
](lggr logger.Logger, maxUnstarted, maxUnconfirmed int) *StateManager[ADDR, SEQ] {
	as := StateManager[ADDR, SEQ]{
		lggr: lggr,

		idempotencyKeyToTx:        map[string]*TX[ADDR, SEQ]{},
		unstartedQueue:            NewPriorityQueue[ADDR, SEQ](maxUnstarted),
		inprogress:                nil,
		unconfirmed:               map[uuid.UUID]*TX[ADDR, SEQ]{},
		availableBroadcasterSpots: make(chan struct{}, 1),
		inflightTransactionSpots:  make(chan struct{}, maxUnconfirmed),

		TxStore: NewInMemoryFileStore[ADDR, SEQ](), // TODO(jtw): REPLACE WITH SOMETHING ELSE
	}

	return &as
}

func (as *StateManager[ADDR, SEQ]) Start(ctx context.Context) {
	// IF THERE ARE AVAILABLE BROADCASTER SPOTS
	// - TRIGGERS SHOULD IMMEDIATELY TRY TO BROADCAST
	// - Backoff timer should trigger a broadcast

	// AVAILABLE WORKERS
	go as.startBroadcasting(ctx)

	<-ctx.Done()
}

func (as *StateManager[ADDR, SEQ]) startBroadcasting(ctx context.Context) {
	var errorRetryCh <-chan time.Time
	bf := newResendBackoff()

	for {
		// broadcast transaction
		retry, err := as.broadcastTransaction(ctx)
		if err == nil {
			// broadcast succeeded so we should try to broadcast again immediately
			continue
		}

		// broadcast failed so we should wait for the next trigger or backoff timer
		as.lggr.Errorw("Error occurred while handling tx queue in state manager", "err", err)
		if retry {
			errorRetryCh = time.After(bf.Duration())
		} else {
			bf = newResendBackoff()
			errorRetryCh = nil
		}

		select {
		case <-ctx.Done():
			return
		case <-as.broadcasterTriggerCh:
			continue
		case <-errorRetryCh:
			continue
		}
	}
}

func (as *StateManager[ADDR, SEQ]) broadcastTransaction(ctx context.Context) (bool /* retry */, error) {
	// GET NEXT UNSTARTED TX
	tx, retry, err := as.moveNextUnstartedToInProgress(ctx)
	if err != nil {
		return retry, err
	}

	// PROCESS TRANSACTION
	errType, err := as.Broadcaster.ProcessTransaction(ctx, tx)
	// TODO: WHAT SHOULD HAPPEN IF THIS FAILS?

	return nil
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

func (as *StateManager[ADDR, SEQ]) moveNextUnstartedToInProgress(ctx context.Context) (*TX[ADDR, SEQ], bool /* retry */, error) {
	if err := as.waitForAvailableInflightTransactionSpot(ctx); err != nil {
		return nil, false, err
	}

	as.lock.Lock()
	defer as.lock.Unlock()

	if as.inprogress != nil {
		as.freeInflightTransactionSpot()
		// TODO: THIS SHOULD IMMEDIATELY TRY TO RESOLVE THE TRANSACTION IN PROGRESS
		return nil, true, fmt.Errorf("move_unstarted_to_in_progress: a transaction is already in progress")
	}

	tx := as.unstartedQueue.RemoveNextTx()
	if tx == nil {
		as.freeInflightTransactionSpot()
		return nil, false, fmt.Errorf("move_unstarted_to_in_progress: no unstarted transaction to move to in_progress")
	}
	tx.State = txmgr.TxInProgress
	// txAttempt, err = client.NewTxAttempt(tx, tx.Sequence)
	tx.TxAttempts = append(tx.TxAttempts, TxAttempt[ADDR]{
		// TODO: THIS NEEDS TO BE FILLED OUT
	})
	// TODO: SET SEQUENCE
	// tx.Sequence = nil

	// PERSIST TX
	if err := as.TxStore.Update(*tx); err != nil {
		tx.State = txmgr.TxUnstarted
		tx.TxAttempts = tx.TxAttempts[:len(tx.TxAttempts)-1]
		tx.Sequence = nil
		as.unstartedQueue.AddTx(tx)
		as.freeInflightTransactionSpot()

		return nil, true, fmt.Errorf("failed to persist transaction: %w", err)
	}
	as.inprogress = tx

	return tx, false, nil
}

func newResendBackoff() backoff.Backoff {
	return backoff.Backoff{
		Min:    1 * time.Second,
		Max:    10 * time.Second,
		Jitter: true,
	}
}

// FindTxWithIdempotencyKey returns the transaction with the given idempotency key.
// If no transaction is found, it returns an error.
func (as *StateManager[ADDR, SEQ]) FindTxWithIdempotencyKey(key string) (TX[ADDR, SEQ], error) {
	as.lock.RLock()
	defer as.lock.RUnlock()

	tx, ok := as.idempotencyKeyToTx[key]
	if !ok {
		return TX[ADDR, SEQ]{}, fmt.Errorf("find_tx_with_idempotency_key: %w", ErrTxnNotFound)
	}

	return *tx, nil
}

// EnqueueUnstartedTransaction takes a transaction and adds it to the unstarted queue.
// If the queue is full, it returns an error.
// If the transaction is able to be added to the queue and it is successfully persisted, it is added to the unstarted queue and returned.
func (as *StateManager[ADDR, SEQ]) EnqueueUnstartedTransaction(tx TX[ADDR, SEQ]) (TX[ADDR, SEQ], error) {
	as.lock.Lock()
	defer as.lock.Unlock()

	// CHECK IF CAPACITY HAS BEEN REACHED
	if as.unstartedQueue.Len() >= as.unstartedQueue.Cap() {
		return TX[ADDR, SEQ]{}, fmt.Errorf("move_tx_to_unstarted: unstarted queue capacity has been reached")
	}

	tx.State = txmgr.TxUnstarted

	// PERSIST TX
	if err := as.TxStore.Create(tx); err != nil {
		return TX[ADDR, SEQ]{}, fmt.Errorf("failed to persist transaction: %w", err)
	}

	as.unstartedQueue.AddTx(&tx)
	if tx.IdempotencyKey != nil {
		as.idempotencyKeyToTx[*tx.IdempotencyKey] = &tx
	}

	return tx, nil
}

func (as *StateManager[ADDR, SEQ]) waitForAvailableInflightTransactionSpot(ctx context.Context) error {
	select {
	// TODO: what happens if something fails before it is unblocked?
	// TODO: THIS MIGHT NEED TO BE CLEANER
	case as.inflightTransactionSpots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (as *StateManager[ADDR, SEQ]) freeInflightTransactionSpot() {
	<-as.inflightTransactionSpots
}

func (as *StateManager[ADDR, SEQ]) Close() {
	as.lock.Lock()
	defer as.lock.Unlock()

	as.unstartedQueue.Close()
	as.unstartedQueue = nil
	as.inprogress = nil
	clear(as.unconfirmed)
	clear(as.idempotencyKeyToTx)
}

func (as *StateManager[ADDR, SEQ]) unstartedCount() int {
	as.lock.RLock()
	defer as.lock.RUnlock()

	return as.unstartedQueue.Len()
}
func (as *StateManager[ADDR, SEQ]) unconfirmedCount() int {
	as.lock.RLock()
	defer as.lock.RUnlock()

	return len(as.unconfirmed)
}

func (as *StateManager[ADDR, SEQ]) peekNextUnstartedTx() (*TX[ADDR, SEQ], error) {
	as.lock.RLock()
	defer as.lock.RUnlock()

	tx := as.unstartedQueue.PeekNextTx()
	if tx == nil {
		return nil, fmt.Errorf("peek_next_unstarted_tx: %w", ErrTxnNotFound)
	}

	return tx, nil
}

func (as *StateManager[ADDR, SEQ]) peekInProgressTx() (*TX[ADDR, SEQ], error) {
	as.lock.RLock()
	defer as.lock.RUnlock()

	tx := as.inprogress
	if tx == nil {
		return nil, fmt.Errorf("peek_in_progress_tx: %w", ErrTxnNotFound)
	}

	return tx, nil
}

func (as *StateManager[ADDR, SEQ]) moveInProgressToUnconfirmed(txAttempt TxAttempt[ADDR]) error {
	as.lock.Lock()
	defer as.lock.Unlock()

	tx := as.inprogress
	if tx == nil {
		return fmt.Errorf("move_in_progress_to_unconfirmed: no transaction in progress")
	}
	tx.State = txmgr.TxUnconfirmed

	var found bool
	for i := 0; i < len(tx.TxAttempts); i++ {
		if tx.TxAttempts[i].UUID == txAttempt.UUID {
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

	as.unconfirmed[tx.UUID] = tx
	as.inprogress = nil

	return nil
}

func (as *StateManager[ADDR, SEQ]) abandon() {
	as.lock.Lock()
	defer as.lock.Unlock()

	for as.unstartedQueue.Len() > 0 {
		tx := as.unstartedQueue.RemoveNextTx()
		tx.State = txmgr.TxFatalError
		tx.Sequence = nil
		tx.Error = ErrTxnAbandoned
	}

	if as.inprogress != nil {
		as.inprogress.State = txmgr.TxFatalError
		as.inprogress.Sequence = nil
		as.inprogress.Error = ErrTxnAbandoned
		as.inprogress = nil
	}
	for _, tx := range as.unconfirmed {
		tx.State = txmgr.TxFatalError
		tx.Sequence = nil
		tx.Error = ErrTxnAbandoned
	}
	for _, tx := range as.idempotencyKeyToTx {
		tx.State = txmgr.TxFatalError
		tx.Sequence = nil
		tx.Error = ErrTxnAbandoned
	}

	clear(as.unconfirmed)
}
