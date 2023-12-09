package x

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	txmgrtypes "github.com/smartcontractkit/chainlink/v2/common/txmgr/types"
	"github.com/smartcontractkit/chainlink/v2/common/types"
	"github.com/smartcontractkit/chainlink/v2/common/x/transaction"
	"github.com/smartcontractkit/chainlink/v2/core/logger"
	"github.com/smartcontractkit/chainlink/v2/core/utils"
)

type AddressStateManager[
	ADDR types.Hashable,
	SEQ types.Sequence,
] struct {
	lggr logger.Logger

	chainID                      string
	maxUnconfirmed, maxUnstarted int

	KeyStore KeyStore[ADDR]
	txConfig txmgrtypes.TransactionManagerTransactionsConfig
	fwdMgr   txmgrtypes.ForwarderManager[ADDR]

	sync.RWMutex
	AddressStateTxManagers map[ADDR]*transaction.StateManager[ADDR, SEQ]
}

func NewAddressStateManager[
	ADDR types.Hashable,
	SEQ types.Sequence,
](
	lggr logger.Logger,
	chainID string,
	keyStore KeyStore[ADDR],
	txConfig txmgrtypes.TransactionManagerTransactionsConfig,
	fwdMgr txmgrtypes.ForwarderManager[ADDR],
) (*AddressStateManager[ADDR, SEQ], error) {
	asm := &AddressStateManager[ADDR, SEQ]{
		lggr:                   lggr,
		chainID:                chainID,
		KeyStore:               keyStore,
		txConfig:               txConfig,
		fwdMgr:                 fwdMgr,
		AddressStateTxManagers: map[ADDR]*transaction.StateManager[ADDR, SEQ]{},

		// TODO: Make these configurable
		maxUnconfirmed: 100,
		maxUnstarted:   50,
	}
	asm.Lock()
	defer asm.Unlock()

	addresses, err := keyStore.EnabledAddressesForChain(chainID)
	if err != nil {
		return nil, fmt.Errorf("new_in_memory_store: %w", err)
	}

	for _, address := range addresses {
		asm.AddressStateTxManagers[address] = transaction.NewStateManager[ADDR, SEQ](asm.lggr, asm.maxUnstarted, asm.maxUnconfirmed)
	}

	return asm, nil
}

func (asm *AddressStateManager[ADDR, SEQ]) Close() {
	asm.Lock()
	defer asm.Unlock()

	for _, as := range asm.AddressStateTxManagers {
		as.Close()
	}
}

func (asm *AddressStateManager[ADDR, SEQ]) CreateTransaction(txRequest transaction.TxRequest[ADDR]) (transaction.TX[ADDR, SEQ], error) {
	asm.Lock()
	defer asm.Unlock()

	// CHECK IF ADDRESS ENABLED
	if err := asm.KeyStore.CheckEnabled(txRequest.FromAddress, asm.chainID); err != nil {
		return transaction.TX[ADDR, SEQ]{}, fmt.Errorf("cannot send transaction from %s on chain ID %s: %w", txRequest.FromAddress, asm.chainID, err)
	}

	// GET ADDRESS TRANSACTION STATE MANAGER
	tm, ok := asm.AddressStateTxManagers[txRequest.FromAddress]
	if !ok {
		tm = transaction.NewStateManager[ADDR, SEQ](asm.lggr, asm.maxUnstarted, asm.maxUnconfirmed)
		asm.AddressStateTxManagers[txRequest.FromAddress] = tm
	}

	// CHECK IDEMPOTENCY KEY
	if txRequest.IdempotencyKey != nil {
		if tx, err := tm.FindTxWithIdempotencyKey(*txRequest.IdempotencyKey); err == nil {
			asm.lggr.Infow("Found a Tx with IdempotencyKey. Returning existing Tx without creating a new one.", "IdempotencyKey", *txRequest.IdempotencyKey)
			return tx, nil
		}
	}

	// CHECK IF FORWARDERS ENABLED AND HANDLE FORWARDERS
	if asm.txConfig.ForwardersEnabled() && (!utils.IsZero(txRequest.ForwarderAddress)) {
		fwdPayload, fwdErr := asm.fwdMgr.ConvertPayload(txRequest.ToAddress, txRequest.EncodedPayload)
		if fwdErr == nil {
			// Handling meta not set at caller.
			if txRequest.Meta != nil {
				txRequest.Meta.FwdrDestAddress = &txRequest.ToAddress
			} else {
				txRequest.Meta = &transaction.TxMeta[ADDR]{
					FwdrDestAddress: &txRequest.ToAddress,
				}
			}

			txRequest.ToAddress = txRequest.ForwarderAddress
			txRequest.EncodedPayload = fwdPayload
		} else {
			asm.lggr.Errorf("Failed to use forwarder set upstream: %w", fwdErr.Error())
		}

	}

	// TODO: DO WE NEED TO CHECK FOR THE PIPELINE TASK RUN ID?
	// TODO: DO WE NEED PRUNING STRATEGIES STILL?

	// Initialize Transaction
	tx := transaction.TX[ADDR, SEQ]{
		UUID:      uuid.New(),
		CreatedAt: time.Now().UnixNano(),

		IdempotencyKey: txRequest.IdempotencyKey,
		From:           txRequest.FromAddress,
		To:             txRequest.ToAddress,
		Value:          txRequest.Value,
		EncodedPayload: txRequest.EncodedPayload,
		FeeLimit:       txRequest.FeeLimit,
		Meta:           txRequest.Meta,
		ChainID:        asm.chainID,
		//MinConfirmations: txRequest.MinConfirmations,
		// PipelineTaskRunID: txRequest.PipelineTaskRunID,
		// TransmitChecker:   txRequest.Checker,
		// SignalCallback:    txRequest.SignalCallback,

		TxAttempts: []transaction.TxAttempt[ADDR]{},
	}

	return tm.EnqueueUnstartedTransaction(tx)
}
