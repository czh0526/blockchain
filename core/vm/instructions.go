package vm

import (
	"errors"
	"fmt"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/common/math"
	"github.com/czh0526/blockchain/crypto"
	"github.com/ethereum/go-ethereum/params"
)

var (
	errWriteProtection       = errors.New("evm: write protection")
	errReturnDataOutOfBounds = errors.New("evm: return data out of bounds")
	errExecutionReverted     = errors.New("evm: execution reverted")
	errMaxCodeSizeExceeded   = errors.New("evm: max code size exceeded")
)

func opStop(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	return nil, nil
}

func opByte(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	th, val := stack.pop(), stack.peek()
	if th.Cmp(common.Big32) < 0 {
		b := math.Byte(val, 32, int(th.Int64()))
		val.SetUint64(uint64(b))
	} else {
		val.SetUint64(0)
	}
	evm.interpreter.intPool.put(th)
	return nil, nil
}

func opAdd(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.pop(), stack.peek()
	math.U256(y.Add(x, y))

	evm.interpreter.intPool.put(x)
	return nil, nil
}

func makePush(size uint64, pushByteSize int) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		codeLen := len(contract.Code)

		startMin := codeLen
		if int(*pc+1) < startMin {
			startMin = int(*pc + 1)
		}

		endMin := codeLen
		if startMin+pushByteSize < endMin {
			endMin = startMin + pushByteSize
		}

		integer := evm.interpreter.intPool.get()
		stack.push(integer.SetBytes(common.RightPadBytes(contract.Code[startMin:endMin], pushByteSize)))

		*pc += size
		return nil, nil
	}
}

func makeDup(size int64) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		stack.dup(evm.interpreter.intPool, int(size))
		return nil, nil
	}
}

func opSha3(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, size := stack.pop(), stack.pop()
	// 得到内存数据
	data := memory.Get(offset.Int64(), size.Int64())
	// sha3 计算
	hash := crypto.Keccak256(data)

	if evm.vmConfig.EnablePreimageRecording {
		evm.StateDB.AddPreimage(common.BytesToHash(hash), data)
	}
	// 返回数据
	stack.push(evm.interpreter.intPool.get().SetBytes(hash))
	// 回收指针
	evm.interpreter.intPool.put(offset, size)
	return nil, nil
}

func opSstore(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// key
	loc := common.BigToHash(stack.pop())
	// value
	val := stack.pop()
	// save storage variable for address
	evm.StateDB.SetState(contract.Address(), loc, common.BigToHash(val))
	// 回收指针（为什么不回收 key）
	evm.interpreter.intPool.put(val)
	return nil, nil
}

func opMstore8(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offptr, valptr := stack.pop(), stack.pop()
	off, val := offptr.Int64(), valptr.Int64()
	memory.store[off] = byte(val & 0xff)

	evm.interpreter.intPool.put(offptr, valptr)
	return nil, nil
}

func opMstore(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	mStart, val := stack.pop(), stack.pop()
	memory.Set(mStart.Uint64(), 32, math.PaddedBigBytes(val, 32))

	evm.interpreter.intPool.put(mStart, val)
	return nil, nil
}

func opMload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset := stack.pop()
	val := evm.interpreter.intPool.get().SetBytes(memory.Get(offset.Int64(), 32))
	stack.push(val)

	evm.interpreter.intPool.put(offset)
	return nil, nil
}

func opReturn(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, size := stack.pop(), stack.pop()
	ret := memory.GetPtr(offset.Int64(), size.Int64())

	evm.interpreter.intPool.put(offset, size)
	return ret, nil
}

func opCaller(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.push(contract.Caller().Big())
	return nil, nil
}

func opAddress(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.push(contract.Address().Big())
	return nil, nil
}

func opSuicide(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	balance := evm.StateDB.GetBalance(contract.Address())
	// 转移 contract.Balance 到 caller.Balance
	evm.StateDB.AddBalance(common.BigToAddress(stack.pop()), balance)

	// 标记为 suicide, 清空 contract.Balance
	evm.StateDB.Suicide(contract.Address())
	return nil, nil
}

// 数据尺寸为 big32
func opCallDataLoad(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.push(evm.interpreter.intPool.get().SetBytes(getDataBig(contract.Input, stack.pop(), big32)))
	return nil, nil
}

func opSload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	loc := common.BigToHash(stack.pop())
	val := evm.StateDB.GetState(contract.Address(), loc).Big()
	stack.push(val)
	return nil, nil
}

func opIszero(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x := stack.peek()
	if x.Sign() > 0 {
		x.SetUint64(0)
	} else {
		x.SetUint64(1)
	}
	return nil, nil
}

func opJumpi(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	pos, cond := stack.pop(), stack.pop()
	if cond.Sign() != 0 {
		if !contract.jumpdests.has(contract.CodeHash, contract.Code, pos) {
			nop := contract.GetOp(pos.Uint64())
			return nil, fmt.Errorf("invalid jump destination (%v) %v", nop, pos)
		}
		*pc = pos.Uint64()
	} else {
		*pc++
	}

	evm.interpreter.intPool.put(pos, cond)
	return nil, nil
}

func opJumpdest(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	return nil, nil
}

// 将参数 Input 复制到 memory
func opCallDataCopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	var (
		memOffset  = stack.pop()
		dataOffset = stack.pop()
		length     = stack.pop()
	)
	memory.Set(memOffset.Uint64(), length.Uint64(), getDataBig(contract.Input, dataOffset, length))

	evm.interpreter.intPool.put(memOffset, dataOffset, length)
	return nil, nil
}

func opMsize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.push(evm.interpreter.intPool.get().SetInt64(int64(memory.Len())))
	return nil, nil
}

func opCallCode(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	evm.interpreter.intPool.put(stack.pop())
	gas := evm.callGasTemp

	addr, value, inOffset, inSize, retOffset, retSize := stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop()
	toAddr := common.BigToAddress(addr)
	value = math.U256(value)
	args := memory.Get(inOffset.Int64(), inSize.Int64())

	if value.Sign() != 0 {
		gas += params.CallStipend
	}
	ret, returnGas, err := evm.CallCode(contract, toAddr, args, gas, value)
	if err != nil {
		stack.push(evm.interpreter.intPool.getZero())
	} else {
		stack.push(evm.interpreter.intPool.get().SetUint64(1))
	}
	if err == nil || err == errExecutionReverted {
		memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}
	contract.Gas += returnGas

	evm.interpreter.intPool.put(addr, value, inOffset, inSize, retOffset, retSize)
	return ret, nil

}
