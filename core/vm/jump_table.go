package vm

import (
	"errors"
	"math/big"

	"github.com/czh0526/blockchain/params"
)

type (
	executionFunc       func(pc *uint64, env *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error)
	gasFunc             func(params.GasTable, *EVM, *Contract, *Stack, *Memory, uint64) (uint64, error)
	stackValidationFunc func(*Stack) error
	memorySizeFunc      func(*Stack) *big.Int
)

var errGasUintOverflow = errors.New("gas uint64 overflow")

type operation struct {
	execute       executionFunc
	gasCost       gasFunc
	validateStack stackValidationFunc
	memorySize    memorySizeFunc

	halts   bool
	jumps   bool
	writes  bool
	valid   bool
	reverts bool
	returns bool
}

var (
	constantinopleInstructionSet = NewConstantinopleInstructionSet()
)

func NewConstantinopleInstructionSet() [256]operation {
	return [256]operation{
		STOP:         {},
		ADD:          {},
		MUL:          {},
		SUB:          {},
		DIV:          {},
		SDIV:         {},
		MOD:          {},
		SMOD:         {},
		ADDMOD:       {},
		MULMOD:       {},
		EXP:          {},
		SIGNEXTEND:   {},
		LT:           {},
		GT:           {},
		SLT:          {},
		SGT:          {},
		EQ:           {},
		ISZERO:       {},
		AND:          {},
		XOR:          {},
		OR:           {},
		NOT:          {},
		BYTE:         {},
		SHA3:         {},
		ADDRESS:      {},
		BALANCE:      {},
		ORIGIN:       {},
		CALLER:       {},
		CALLVALUE:    {},
		CALLDATALOAD: {},
		CALLDATASIZE: {},
		CALLDATACOPY: {},
		CODESIZE:     {},
		CODECOPY:     {},
		GASPRICE:     {},
		EXTCODESIZE:  {},
		EXTCODECOPY:  {},
		BLOCKHASH:    {},
		COINBASE:     {},
		TIMESTAMP:    {},
		NUMBER:       {},
		DIFFICULTY:   {},
		GASLIMIT:     {},
		POP:          {},
		MLOAD:        {},
		MSTORE:       {},
		MSTORE8:      {},
		SLOAD:        {},
		SSTORE:       {},
		JUMP:         {},
		JUMPI:        {},
		PC:           {},
		MSIZE:        {},
		GAS:          {},
		JUMPDEST:     {},
		PUSH1:        {},
		PUSH2:        {},
		PUSH3:        {},
		PUSH4:        {},
		PUSH5:        {},
		PUSH6:        {},
		PUSH7:        {},
		PUSH8:        {},
		PUSH9:        {},
		PUSH10:       {},
		PUSH11:       {},
		PUSH12:       {},
		PUSH13:       {},
		PUSH14:       {},
		PUSH15:       {},
		PUSH16:       {},
		PUSH17:       {},
		PUSH18:       {},
		PUSH19:       {},
		PUSH20:       {},
		PUSH21:       {},
		PUSH22:       {},
		PUSH23:       {},
		PUSH24:       {},
		PUSH25:       {},
		PUSH26:       {},
		PUSH27:       {},
		PUSH28:       {},
		PUSH29:       {},
		PUSH30:       {},
		PUSH31:       {},
		PUSH32:       {},
		DUP1:         {},
		DUP2:         {},
		DUP3:         {},
		DUP4:         {},
		DUP5:         {},
		DUP6:         {},
		DUP7:         {},
		DUP8:         {},
		DUP9:         {},
		DUP10:        {},
		DUP11:        {},
		DUP12:        {},
		DUP13:        {},
		DUP14:        {},
		DUP15:        {},
		DUP16:        {},
		SWAP1:        {},
		SWAP2:        {},
		SWAP3:        {},
		SWAP4:        {},
		SWAP5:        {},
		SWAP6:        {},
		SWAP7:        {},
		SWAP8:        {},
		SWAP9:        {},
		SWAP10:       {},
		SWAP11:       {},
		SWAP12:       {},
		SWAP13:       {},
		SWAP14:       {},
		SWAP15:       {},
		SWAP16:       {},
		LOG0:         {},
		LOG1:         {},
		LOG2:         {},
		LOG3:         {},
		LOG4:         {},
		CREATE:       {},
		CALL:         {},
		CALLCODE:     {},
		RETURN:       {},
		SELFDESTRUCT: {},
	}
}
