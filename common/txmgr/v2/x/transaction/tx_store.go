package transaction

import (
	"github.com/smartcontractkit/chainlink/v2/common/types"
	"github.com/spf13/afero"
)

type FileStore[
	ADDR types.Hashable,
	SEQ types.Sequence,
] struct {
	fs afero.Fs
}

func NewInMemoryFileStore[
	ADDR types.Hashable,
	SEQ types.Sequence,
]() *FileStore[ADDR, SEQ] {
	return &FileStore[ADDR, SEQ]{
		fs: afero.NewMemMapFs(),
	}
}

func NewFileStore[
	ADDR types.Hashable,
	SEQ types.Sequence,
]() *FileStore[ADDR, SEQ] {
	return &FileStore[ADDR, SEQ]{
		fs: afero.NewOsFs(),
	}
}

func (fs *FileStore[ADDR, SEQ]) Create(tx TX[ADDR, SEQ]) (err error) {
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

func (fs *FileStore[ADDR, SEQ]) Update(tx TX[ADDR, SEQ]) error {
	return nil
}

func (fs *FileStore[ADDR, SEQ]) Delete(tx TX[ADDR, SEQ]) error {
	return nil
}
