package evm

import (
	"fmt"

	commontypes "github.com/smartcontractkit/chainlink-common/pkg/types"
)

type bindings map[string]map[string]binding

func (b bindings) GetBinding(contractName, readName string) (binding, error) {
	methodReaders, ok := b[contractName]
	if !ok {
		return nil, fmt.Errorf("%w: no contract named %s", commontypes.ErrInvalidType, contractName)
	}

	reader, ok := methodReaders[readName]
	if !ok {
		return nil, fmt.Errorf("%w: no readName named %s in contract%s", commontypes.ErrInvalidType, readName, contractName)
	}
	return reader, nil
}

func (b bindings) AddBinding(contractName, readName string, reader binding) {
	mb, ok := b[contractName]
	if !ok {
		b[contractName] = map[string]binding{}
	}
	mb[readName] = reader
}

func (b bindings) ForEach(fn func(binding) error) error {
	for _, readers := range b {
		for _, reader := range readers {
			if err := fn(reader); err != nil {
				return err
			}
		}
	}
	return nil
}
