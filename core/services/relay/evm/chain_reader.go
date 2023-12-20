package evm

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	commonservices "github.com/smartcontractkit/chainlink-common/pkg/services"
	commontypes "github.com/smartcontractkit/chainlink-common/pkg/types"

	evmclient "github.com/smartcontractkit/chainlink/v2/core/chains/evm/client"
	"github.com/smartcontractkit/chainlink/v2/core/chains/legacyevm"

	"github.com/smartcontractkit/chainlink/v2/core/chains/evm/logpoller"
	"github.com/smartcontractkit/chainlink/v2/core/logger"
	"github.com/smartcontractkit/chainlink/v2/core/services"
	"github.com/smartcontractkit/chainlink/v2/core/services/relay/evm/types"
)

type ChainReaderService interface {
	services.ServiceCtx
	commontypes.ChainReader
}

type chainReader struct {
	lggr     logger.Logger
	lp       logpoller.LogPoller
	client   evmclient.Client
	bindings bindings
	parsed   *parsedTypes
	codec    commontypes.RemoteCodec
	commonservices.StateMachine
}

// NewChainReaderService is a constructor for ChainReader, returns nil if there is any error
func NewChainReaderService(lggr logger.Logger, lp logpoller.LogPoller, chain legacyevm.Chain, config types.ChainReaderConfig) (ChainReaderService, error) {
	cr := &chainReader{
		lggr:     lggr.Named("ChainReader"),
		lp:       lp,
		client:   chain.Client(),
		bindings: map[string]map[string]binding{},
	}

	var err error
	if err = cr.init(config.ChainContractReaders); err != nil {
		return nil, err
	}

	if cr.codec, err = cr.parsed.toCodec(); err != nil {
		return nil, err
	}

	return cr, nil
}

func (cr *chainReader) Name() string { return cr.lggr.Name() }

var _ commontypes.ContractTypeProvider = &chainReader{}

func (cr *chainReader) GetLatestValue(ctx context.Context, contractName, method string, params any, returnVal any) error {
	b, err := cr.bindings.GetBinding(contractName, method)
	if err != nil {
		return err
	}

	bytes, err := b.GetLatestValue(ctx, params)
	if err != nil {
		return err
	}
	return cr.codec.Decode(ctx, bytes, returnVal, wrapItemType(contractName, method, false))
}

func (cr *chainReader) init(chainContractReaders map[string]types.ChainContractReader) error {
	for contractName, chainContractReader := range chainContractReaders {
		contractAbi, err := abi.JSON(strings.NewReader(chainContractReader.ContractABI))
		if err != nil {
			return err
		}

		for typeName, chainReaderDefinition := range chainContractReader.ChainReaderDefinitions {
			switch chainReaderDefinition.ReadType {
			case types.Method:
				err = cr.addMethod(contractName, typeName, contractAbi, chainReaderDefinition)
			case types.Event:
				err = cr.addEvent(contractName, typeName, contractAbi, chainReaderDefinition)
			default:
				return fmt.Errorf(
					"%w: invalid chain reader definition read type: %d",
					commontypes.ErrInvalidConfig,
					chainReaderDefinition.ReadType)
			}

			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (cr *chainReader) Start(_ context.Context) error {
	return cr.StartOnce("ChainReader", func() error {
		return cr.bindings.ForEach(binding.Register)
	})
}

func (cr *chainReader) Close() error {
	return cr.StopOnce("ChainReader", func() error {
		return cr.bindings.ForEach(binding.Unregister)
	})
}

func (cr *chainReader) Ready() error { return nil }
func (cr *chainReader) HealthReport() map[string]error {
	return map[string]error{cr.Name(): nil}
}

func (cr *chainReader) CreateContractType(contractName, methodName string, forEncoding bool) (any, error) {
	return cr.codec.CreateType(wrapItemType(contractName, methodName, forEncoding), forEncoding)
}

func wrapItemType(contractName, methodName string, isParams bool) string {
	if isParams {
		return fmt.Sprintf("params.%s.%s", contractName, methodName)
	}
	return fmt.Sprintf("return.%s.%s", contractName, methodName)
}

func (cr *chainReader) addMethod(
	contractName,
	methodName string,
	abi abi.ABI,
	chainReaderDefinition types.ChainReaderDefinition) error {
	method, methodExists := abi.Methods[chainReaderDefinition.ChainSpecificName]
	if !methodExists {
		return fmt.Errorf("%w: method %s doesn't exist", commontypes.ErrInvalidConfig, chainReaderDefinition.ChainSpecificName)
	}

	cr.bindings.AddBinding(contractName, methodName, &methodBinding{
		contractName: contractName,
		method:       methodName,
		client:       cr.client,
	})

	if err := cr.addEncoderDef(contractName, methodName, method, chainReaderDefinition); err != nil {
		return err
	}

	return cr.addDecoderDef(contractName, methodName, method.Outputs, chainReaderDefinition)
}

func (cr *chainReader) addEvent(contractName, eventName string, abi abi.ABI, chainReaderDefinition types.ChainReaderDefinition) error {
	event, methodExists := abi.Events[chainReaderDefinition.ChainSpecificName]
	if !methodExists {
		return fmt.Errorf("%w: method %s doesn't exist", commontypes.ErrInvalidConfig, chainReaderDefinition.ChainSpecificName)
	}
	cr.bindings.AddBinding(contractName, eventName, &eventBinding{
		lp:   cr.lp,
		hash: event.ID,
	})
	return cr.addDecoderDef(contractName, eventName, event.Inputs, chainReaderDefinition)
}

func (cr *chainReader) addEncoderDef(contractName, methodName string, method abi.Method, chainReaderDefinition types.ChainReaderDefinition) error {
	// ABI.Pack prepends the method.ID to the encodings, we'll need the encoder to do the same.
	input := &codecEntry{Args: method.Inputs, encodingPrefix: method.ID}

	if err := input.Init(); err != nil {
		return err
	}

	inputMod, err := chainReaderDefinition.InputModifications.ToModifier(evmDecoderHooks...)
	if err != nil {
		return err
	}
	input.mod = inputMod
	cr.parsed.encoderDefs[wrapItemType(contractName, methodName, true)] = input
	return nil
}

func (cr *chainReader) addDecoderDef(contractName, methodName string, outputs abi.Arguments, def types.ChainReaderDefinition) error {
	output := &codecEntry{Args: outputs}
	mod, err := def.OutputModifications.ToModifier(evmDecoderHooks...)
	if err != nil {
		return err
	}
	output.mod = mod
	cr.parsed.decoderDefs[wrapItemType(contractName, methodName, false)] = output
	return output.Init()
}
