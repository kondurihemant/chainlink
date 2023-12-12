package transaction

import (
	feetypes "github.com/smartcontractkit/chainlink/v2/common/fee/types"
	txmgrtypes "github.com/smartcontractkit/chainlink/v2/common/txmgr/types"
	"github.com/smartcontractkit/chainlink/v2/common/types"
	"github.com/spf13/afero"
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

func (fs *FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Create(
	tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE],
) error {
	/*
		// NOTE: ATOMIC WRITE

		fi, err := fs.fs.Stat("test.txt")
		if err == nil && fi.Mode().IsRegular() {
			return fmt.Errorf("file exists and is not a regular file: %s", "test.txt")
		}
		f, err := fs.fs.CreateTemp("dir", "test.txt"+".tmp")
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
		if _, err := f.Write([]byte("content")); err != nil {
			return err
		}
		// TODO: CHECK IF PERMISSIONS ARE CORRECT
		if runtime.GOOS != "windows" {
			if err := f.Chmod(0644); err != nil {
				return err
			}
		}
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return fs.fs.Rename(tmp, "test.txt")
	*/
	return nil
}

func (fs *FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Update(tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) error {
	return nil
}

func (fs *FileStore[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) Delete(tx txmgrtypes.Tx[CHAIN_ID, ADDR, TX_HASH, BLOCK_HASH, SEQ, FEE]) error {
	return nil
}
