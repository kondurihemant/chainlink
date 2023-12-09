package x

import (
	"context"

	"github.com/smartcontractkit/chainlink/v2/common/client"
	"github.com/smartcontractkit/chainlink/v2/common/types"
	"github.com/smartcontractkit/chainlink/v2/common/x/transaction"
	"github.com/smartcontractkit/chainlink/v2/core/logger"
)

type ChainClient[
	ADDR types.Hashable,
	SEQ types.Sequence,
] interface {
	SendTransactionReturnCode(ctx context.Context, tx transaction.TX[ADDR, SEQ], attempt transaction.TxAttempt[ADDR], logger logger.Logger) (client.SendTxReturnCode, error)
}

// Broadcaster is responsible for receiving UNSTARTED transactions and queing them up for eventual
// validation, preparation, and broadcast to chain.
type Broadcaster[
	ADDR types.Hashable,
	SEQ types.Sequence,
] struct {
	logger logger.Logger
	client ChainClient[ADDR, SEQ]
}

// NewBroadcaster returns a new Broadcaster.
func NewBroadcaster[
	ADDR types.Hashable,
	SEQ types.Sequence,
](logger logger.Logger) *Broadcaster[ADDR, SEQ] {
	return &Broadcaster[ADDR, SEQ]{
		logger: logger,
	}
}

// ProcessTransactions processes transactions from the queue.
// Transactions are considered in the IN_PROGRESS state.
func (b *Broadcaster[ADDR, SEQ]) ProcessTransaction(ctx context.Context, tx transaction.TX[ADDR, SEQ]) (client.SendTxReturnCode, error) {

	// VALIDATE TRANSACTIONS
	// BROADCAST TRANSACTIONS

	txAttempt := tx.TxAttempts[len(tx.TxAttempts)-1]

	return b.client.SendTransactionReturnCode(ctx, tx, txAttempt, b.logger)
}
