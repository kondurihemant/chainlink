package x

import (
	"fmt"

	txmgrtypes "github.com/smartcontractkit/chainlink/v2/common/txmgr/types"
	"github.com/smartcontractkit/chainlink/v2/common/types"
	"github.com/smartcontractkit/chainlink/v2/common/x/transaction"
	"github.com/smartcontractkit/chainlink/v2/core/logger"
)

var (
	// ErrAddressNotFound ...
	ErrAddressNotFound = fmt.Errorf("address not found")
)

// KeyStore ...
type KeyStore[
	ADDR types.Hashable,
] interface {
	EnabledAddressesForChain(chainID string) ([]ADDR, error)
	CheckEnabled(address ADDR, chainID string) error
}

// Txm ...
type Txm[
	ADDR types.Hashable,
	SEQ types.Sequence,
] struct {
	lggr    logger.Logger
	chainID string

	KeyStore            KeyStore[ADDR]
	Broadcaster         *Broadcaster[ADDR, SEQ]
	AddressStateManager *AddressStateManager[ADDR, SEQ]
}

// NewTxm ...
func NewTxm[
	ADDR types.Hashable,
	SEQ types.Sequence,
](
	lggr logger.Logger,
	chainID string,
	keyStore KeyStore[ADDR],

	txConfig txmgrtypes.TransactionManagerTransactionsConfig,
	fwdMgr txmgrtypes.ForwarderManager[ADDR],
) (*Txm[ADDR, SEQ], error) {
	txm := &Txm[ADDR, SEQ]{
		lggr:        lggr,
		chainID:     chainID,
		Broadcaster: NewBroadcaster[ADDR, SEQ](lggr),
	}

	asm, err := NewAddressStateManager[ADDR, SEQ](lggr, chainID, keyStore, txConfig, fwdMgr)
	if err != nil {
		return nil, fmt.Errorf("new_txm: %w", err)
	}
	txm.AddressStateManager = asm

	return txm, nil
}

// CreateTransaction ...
func (t *Txm[ADDR, SEQ]) CreateTransaction(txRequest transaction.TxRequest[ADDR]) (transaction.TX[ADDR, SEQ], error) {
	return t.AddressStateManager.CreateTransaction(txRequest)
}
