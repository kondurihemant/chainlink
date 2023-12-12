package transaction

import (
	"encoding/json"
	"path/filepath"
	"runtime"

	"github.com/gofrs/uuid"
	feetypes "github.com/smartcontractkit/chainlink/v2/common/fee/types"
	txmgrtypes "github.com/smartcontractkit/chainlink/v2/common/txmgr/types"
	"github.com/smartcontractkit/chainlink/v2/common/types"
	"github.com/spf13/afero"
)

var (
	ErrFileNotFound = afero.ErrFileNotFound
)

type FileStore[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
] struct {
	fs afero.Fs
}

func NewInMemoryFileStore[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
]() *FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE] {
	return &FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{
		fs: afero.NewMemMapFs(),
	}
}

func NewFileStore[
	CHAIN_ID types.ID,
	ADDR types.Hashable,
	TX_HASH types.Hashable,
	BLOCK_HASH types.Hashable,
	SEQ types.Sequence,
	FEE feetypes.Fee,
]() *FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE] {
	return &FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]{
		fs: afero.NewOsFs(),
	}
}

func (fs *FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Upsert(tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) error {
	filename := tx.ChainID.String() + "/" + tx.UUID.String() + ".json"

	f, err := afero.TempFile(fs.fs, filepath.Dir(filename), filepath.Base(filename)+".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			fs.fs.Remove(tmp)
		}
	}()

	data, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}

	if runtime.GOOS != "windows" {
		if err := fs.fs.Chmod(tmp, 0644); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return fs.fs.Rename(tmp, filename)
}

func (fs *FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Get(chainID CHAIN_ID, uuid uuid.UUID) (tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE], err error) {
	filename := chainID.String() + "/" + uuid.String() + ".json"

	// Check if file exists
	fileExists, err := afero.Exists(fs.fs, filename)
	if err != nil {
		return tx, err
	}
	if !fileExists {
		return tx, ErrFileNotFound
	}

	// Read file
	f, err := fs.fs.Open(filename)
	if err != nil {
		return tx, err
	}
	defer f.Close()

	if err = json.NewDecoder(f).Decode(&tx); err != nil {
		return tx, err
	}

	return tx, nil
}

func (fs *FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Delete(tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) error {
	filename := tx.ChainID.String() + "/" + tx.UUID.String() + ".json"

	return fs.fs.Remove(filename)
}
