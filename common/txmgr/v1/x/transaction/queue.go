package transaction

import (
	"container/heap"
	"sync"

	"github.com/google/uuid"

	"github.com/smartcontractkit/chainlink/v2/common/types"
)

// PriorityQueue is a priority queue of transactions prioritized by creation time. The oldest transaction is at the front of the queue.
// RESPONSIBILITIES:
// - INSERTING TRANSACTIONS INTO A QUEUE BASED ON SPECIFIC RULES
// - RETURNING TRANSACTIONS FROM A QUEUE BASED ON SPECIFIC RULES
// - HANDLING QUEUE CAPACITY CHECKS
// - HANDLING PRUNING STRATEGY
type PriorityQueue[
	ADDR types.Hashable,
	SEQ types.Sequence,
] struct {
	sync.Mutex
	txs []*TX[ADDR, SEQ]
}

// NewPriorityQueue returns a new PriorityQueue instance
func NewPriorityQueue[
	ADDR types.Hashable,
	SEQ types.Sequence,
](maxUnstarted int) *PriorityQueue[ADDR, SEQ] {
	pq := PriorityQueue[ADDR, SEQ]{
		txs: make([]*TX[ADDR, SEQ], 0, maxUnstarted),
	}

	return &pq
}

func (pq *PriorityQueue[ADDR, SEQ]) Cap() int {
	return cap(pq.txs)
}
func (pq *PriorityQueue[ADDR, SEQ]) Len() int {
	return len(pq.txs)
}
func (pq *PriorityQueue[ADDR, SEQ]) Less(i, j int) bool {
	// We want Pop to give us the oldest, not newest, transaction based on creation time
	return pq.txs[i].CreatedAt < pq.txs[j].CreatedAt
}
func (pq *PriorityQueue[ADDR, SEQ]) Swap(i, j int) {
	pq.txs[i], pq.txs[j] = pq.txs[j], pq.txs[i]
}
func (pq *PriorityQueue[ADDR, SEQ]) Push(tx any) {
	pq.txs = append(pq.txs, tx.(*TX[ADDR, SEQ]))
}
func (pq *PriorityQueue[ADDR, SEQ]) Pop() any {
	pq.Lock()
	defer pq.Unlock()

	old := pq.txs
	n := len(old)
	tx := old[n-1]
	old[n-1] = nil // avoid memory leak
	pq.txs = old[0 : n-1]
	return tx
}

func (pq *PriorityQueue[ADDR, SEQ]) AddTx(tx *TX[ADDR, SEQ]) {
	pq.Lock()
	defer pq.Unlock()

	heap.Push(pq, tx)
}
func (pq *PriorityQueue[ADDR, SEQ]) RemoveNextTx() *TX[ADDR, SEQ] {
	pq.Lock()
	defer pq.Unlock()

	return heap.Pop(pq).(*TX[ADDR, SEQ])
}
func (pq *PriorityQueue[ADDR, SEQ]) RemoveTxByID(uuid uuid.UUID) *TX[ADDR, SEQ] {
	pq.Lock()
	defer pq.Unlock()

	for i, tx := range pq.txs {
		if tx.UUID == uuid {
			return heap.Remove(pq, i).(*TX[ADDR, SEQ])
		}
	}

	return nil
}
func (pq *PriorityQueue[ADDR, SEQ]) PeekNextTx() *TX[ADDR, SEQ] {
	if len(pq.txs) == 0 {
		return nil
	}
	return pq.txs[0]
}
func (pq *PriorityQueue[ADDR, SEQ]) Close() {
	pq.Lock()
	defer pq.Unlock()

	clear(pq.txs)
}
