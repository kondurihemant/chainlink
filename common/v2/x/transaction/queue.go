package transaction

import (
	"container/heap"
	"sync"

	feetypes "github.com/smartcontractkit/chainlink/v2/common/fee/types"
	txmgrtypes "github.com/smartcontractkit/chainlink/v2/common/txmgr/types"
	"github.com/smartcontractkit/chainlink/v2/common/types"
)

// PriorityQueue is a priority queue of transactions prioritized by creation time. The oldest transaction is at the front of the queue.
// RESPONSIBILITIES:
// - INSERTING TRANSACTIONS INTO A QUEUE BASED ON SPECIFIC RULES
// - RETURNING TRANSACTIONS FROM A QUEUE BASED ON SPECIFIC RULES
// - HANDLING QUEUE CAPACITY CHECKS
// - HANDLING PRUNING STRATEGY
type PriorityQueue[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
] struct {
	sync.Mutex
	txs       []*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]
	idToIndex map[int64]int
}

// NewPriorityQueue returns a new PriorityQueue instance
func NewPriorityQueue[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
](maxUnstarted int) *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE] {
	pq := PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{
		txs:       make([]*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], 0, maxUnstarted),
		idToIndex: make(map[int64]int),
	}

	return &pq
}

func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Cap() int {
	return cap(pq.txs)
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Len() int {
	return len(pq.txs)
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Less(i, j int) bool {
	// We want Pop to give us the oldest, not newest, transaction based on creation time
	return pq.txs[i].CreatedAt.Before(pq.txs[j].CreatedAt)
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Swap(i, j int) {
	pq.txs[i], pq.txs[j] = pq.txs[j], pq.txs[i]
	pq.idToIndex[pq.txs[i].ID] = i
	pq.idToIndex[pq.txs[j].ID] = j
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Push(tx any) {
	pq.txs = append(pq.txs, tx.(*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]))
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Pop() any {
	pq.Lock()
	defer pq.Unlock()

	old := pq.txs
	n := len(old)
	tx := old[n-1]
	old[n-1] = nil // avoid memory leak
	pq.txs = old[0 : n-1]
	return tx
}

func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) AddTx(tx *txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) {
	pq.Lock()
	defer pq.Unlock()

	heap.Push(pq, tx)
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) RemoveNextTx() *txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE] {
	pq.Lock()
	defer pq.Unlock()

	return heap.Pop(pq).(*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE])
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) RemoveTxByID(id int64) *txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE] {
	pq.Lock()
	defer pq.Unlock()

	if i, ok := pq.idToIndex[id]; ok {
		return heap.Remove(pq, i).(*txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE])
	}

	return nil
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) PeekNextTx() *txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE] {
	if len(pq.txs) == 0 {
		return nil
	}
	return pq.txs[0]
}
func (pq *PriorityQueue[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Close() {
	pq.Lock()
	defer pq.Unlock()

	clear(pq.txs)
}
