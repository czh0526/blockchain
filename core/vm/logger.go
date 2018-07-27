package vm

import (
	"math/big"
	"time"

	"github.com/czh0526/blockchain/common"
)

type Tracer interface {
	CaptureStart(from common.Address, to common.Address, call bool, input []byte, gas uint64, value *big.Int) error
	CaptureState(env *EVM, pc uint64, op OpCode, gas, cost uint64, memory *Memory, stack *Stack, contract *Contract, depth int, err error) error
	CaptureFault(env *EVM, pc uint64, op OpCode, gas, cost uint64, memory *Memory, stack *Stack, contract *Contract, depth int, err error) error
	CaptureEnd(output []byte, gasUsed uint64, t time.Duration, err error) error
}
