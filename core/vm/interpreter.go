package vm

import (
	"fmt"
	"sync/atomic"

	"github.com/czh0526/blockchain/common/math"

	"github.com/czh0526/blockchain/params"
)

type Config struct {
	Debug                   bool
	Tracer                  Tracer
	NoRecursion             bool
	EnablePreimageRecording bool
	JumpTable               [256]operation
}

type Interpreter struct {
	evm        *EVM
	cfg        Config
	gasTable   params.GasTable
	intPool    *intPool
	readOnly   bool
	returnData []byte
}

func NewInterpreter(evm *EVM, cfg Config) *Interpreter {
	if !cfg.JumpTable[STOP].valid {
		cfg.JumpTable = constantinopleInstructionSet
	}

	return &Interpreter{
		evm:      evm,
		cfg:      cfg,
		gasTable: evm.ChainConfig().GasTable(evm.BlockNumber),
		intPool:  newIntPool(),
	}
}

func (in *Interpreter) Run(contract *Contract, input []byte) (ret []byte, err error) {
	// 标记调用栈的深度
	in.evm.depth++
	defer func() {
		in.evm.depth--
	}()

	in.returnData = nil

	if len(contract.Code) == 0 {
		return nil, nil
	}

	var (
		op    OpCode
		mem   = NewMemory()
		stack = newstack()

		pc   = uint64(0)
		cost uint64

		pcCopy  uint64
		gasCopy uint64
		logged  bool
	)

	contract.Input = input

	if in.cfg.Debug {
		defer func() {
			if err != nil {
				if !logged {
					in.cfg.Tracer.CaptureState(in.evm, pcCopy, op, gasCopy, cost, mem, stack, contract, in.evm.depth, err)
				} else {
					in.cfg.Tracer.CaptureFault(in.evm, pcCopy, op, gasCopy, cost, mem, stack, contract, in.evm.depth, err)
				}
			}
		}()
	}

	// 循环执行每一条指令
	for atomic.LoadInt32(&in.evm.abort) == 0 {
		if in.cfg.Debug {
			logged, pcCopy, gasCopy = false, pc, contract.Gas
		}

		// 获取指令
		op = contract.GetOp(pc)
		operation := in.cfg.JumpTable[op]
		if !operation.valid {
			return nil, fmt.Errorf("invalid opcode 0x%x", int(op))
		}

		// 检查堆栈
		if err := operation.validateStack(stack); err != nil {
			return nil, err
		}

		if err := in.enforceRestrictions(op, operation, stack); err != nil {
			return nil, err
		}

		// 计算使用的内存空间
		var memorySize uint64
		if operation.memorySize != nil {
			memSize, overflow := bigUint64(operation.memorySize(stack))
			if overflow {
				return nil, errGasUintOverflow
			}

			if memorySize, overflow = math.SafeMul(toWordSize(memSize), 32); overflow {
				return nil, errGasUintOverflow
			}
		}

		// 计算 gas = memory gas + operation gas
		cost, err = operation.gasCost(in.gasTable, in.evm, contract, stack, mem, memorySize)
		if err != nil || !contract.UseGas(cost) {
			return nil, ErrOutOfGas
		}
		if memorySize > 0 {
			mem.Resize(memorySize)
		}

		if in.cfg.Debug {
			in.cfg.Tracer.CaptureState(in.evm, pc, op, gasCopy, cost, mem, stack, contract, in.evm.depth, err)
			logged = true
		}

		// 执行
		res, err := operation.execute(&pc, in.evm, contract, mem, stack)
		/*
			commented for testing
			if verifyPool {
				verifyIntegerPool(in.intPool)
			}
		*/

		if operation.returns {
			in.returnData = res
		}

		switch {
		case err != nil:
			return nil, err
		case operation.reverts:
			return res, errExecutionReverted
		case operation.halts: // 停止
			return res, nil
		case !operation.jumps: // 循环
			pc++
		}
	}
	return nil, nil
}

func (in *Interpreter) enforceRestrictions(op OpCode, operation operation, stack *Stack) error {
	if in.readOnly {
		if operation.writes || (op == CALL && stack.Back(2).BitLen() > 0) {
			return errWriteProtection
		}
	}
	return nil
}
