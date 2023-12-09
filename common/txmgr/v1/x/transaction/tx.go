package transaction

import (
	"math/big"

	"github.com/google/uuid"

	txmgrtypes "github.com/smartcontractkit/chainlink/v2/common/txmgr/types"
	"github.com/smartcontractkit/chainlink/v2/common/types"
)

// TX ...
type TX[
	ADDR types.Hashable,
	SEQ types.Sequence,
] struct {
	UUID           uuid.UUID
	IdempotencyKey *string
	Sequence       *SEQ
	From, To       ADDR
	EncodedPayload []byte
	Value          big.Int
	FeeLimit       uint32
	Error          error

	BroadcastAt       *int64
	InitalBroadcastAt *int64
	CreatedAt         int64
	State             txmgrtypes.TxState

	TxAttempts []TxAttempt[ADDR] `json:"-"`

	Meta *TxMeta[ADDR]
	// Subject uuid.NullUUID
	ChainID string

	// PipelineTaskRunID uuid.NullUUID
	// MinConfirmations  clnull.Uint32

	// TransmitChecker defines the check that should be performed before a transaction is submitted on
	// chain.
	// TransmitChecker *sqlutil.JSON

	// Marks tx requiring callback
	// SignalCallback bool
	// Marks tx callback as signaled
	// CallbackCompleted bool
}

// TxAttempt ...
type TxAttempt[
	ADDR types.Hashable,
] struct {
	UUID uuid.UUID
	// Tx    TX[ADDR, SEQ]
	TxFee uint64

	ChainSpecificFeeLimit uint32
	SignedRawTx           []byte
	// Hash				TX_HASH

	CreatedAt               int64
	BroadcastBeforeBlockNum *int64
	State                   txmgrtypes.TxAttemptState
	// Receipts                []txmgrtypes.TxReceipt `json:"-"`
	// TxType                  int
}

// TxRequest ...
type TxRequest[
	ADDR types.Hashable,
] struct {
	IdempotencyKey   *string
	FromAddress      ADDR
	ToAddress        ADDR
	EncodedPayload   []byte
	Value            big.Int
	FeeLimit         uint32
	Meta             *TxMeta[ADDR]
	ForwarderAddress ADDR

	// MinConfirmations clnull.Uint32
	// PipelineTaskRunID *uuid.UUID

	Strategy txmgrtypes.TxStrategy

	Checker TransmitCheckerSpec[ADDR]

	SignalCallback bool
}

// TxMeta ...
type TxMeta[
	ADDR types.Hashable,
] struct {
	FwdrDestAddress *ADDR `json:"ForwarderDestAddress,omitempty"`
}

// TransmitCheckerSpec ...
type TransmitCheckerSpec[
	ADDR types.Hashable,
] struct {
	CheckerType           TransmitCheckerType `json:",omitempty"`
	VRFCoordinatorAddress *ADDR               `json:",omitempty"`
	VRFRequestBlockNumber *big.Int            `json:",omitempty"`
}

// TransmitCheckerType ...
type TransmitCheckerType string
