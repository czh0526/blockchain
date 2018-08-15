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
		STOP: {
			execute:       opStop,
			gasCost:       constGasFunc(0),
			validateStack: makeStackFunc(0, 0),
			halts:         true,
			valid:         true,
		},
		ADD:        {},
		MUL:        {},
		SUB:        {},
		DIV:        {},
		SDIV:       {},
		MOD:        {},
		SMOD:       {},
		ADDMOD:     {},
		MULMOD:     {},
		EXP:        {},
		SIGNEXTEND: {},
		LT:         {},
		GT:         {},
		SLT:        {},
		SGT:        {},
		EQ:         {},
		ISZERO: {
			execute:       opIszero,
			gasCost:       constGasFunc(GasFastestStep),
			validateStack: makeStackFunc(1, 1),
			valid:         true,
		},
		AND:  {},
		XOR:  {},
		OR:   {},
		NOT:  {},
		BYTE: {},
		SHA3: {
			execute:       opSha3,
			gasCost:       gasSha3,
			validateStack: makeStackFunc(2, 1), // 弹2压1
			memorySize:    memorySha3,
			valid:         true,
		},
		ADDRESS: {
			execute:       opAddress,
			gasCost:       constGasFunc(GasQuickStep),
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		BALANCE: {},
		ORIGIN:  {},
		CALLER: {
			execute:       opCaller,
			gasCost:       constGasFunc(GasQuickStep),
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		CALLVALUE: {},
		CALLDATALOAD: {
			execute:       opCallDataLoad,
			gasCost:       constGasFunc(GasFastestStep),
			validateStack: makeStackFunc(1, 1),
			valid:         true,
		},
		CALLDATASIZE: {},
		CALLDATACOPY: {
			execute:       opCallDataCopy,
			gasCost:       gasCallDataCopy,
			validateStack: makeStackFunc(3, 0),
			memorySize:    memoryCallDataCopy,
			valid:         true,
		},
		CODESIZE:    {},
		CODECOPY:    {},
		GASPRICE:    {},
		EXTCODESIZE: {},
		EXTCODECOPY: {},
		BLOCKHASH:   {},
		COINBASE:    {},
		TIMESTAMP:   {},
		NUMBER:      {},
		DIFFICULTY:  {},
		GASLIMIT:    {},
		POP:         {},
		MLOAD: {
			execute:       opMload,
			gasCost:       gasMLoad,
			validateStack: makeStackFunc(1, 1),
			memorySize:    memoryMLoad,
			valid:         true,
		},
		MSTORE: { // 32 bytes | 256 bits
			execute:       opMstore,
			gasCost:       gasMStore,
			memorySize:    memoryMStore,
			validateStack: makeStackFunc(2, 0),
			valid:         true,
		},
		MSTORE8: { // 1 byte | 8 bits
			execute:       opMstore8,
			gasCost:       gasMStore8,
			memorySize:    memoryMStore8,
			validateStack: makeStackFunc(2, 0),
			valid:         true,
		},
		SLOAD: {
			execute:       opSload,
			gasCost:       gasSLoad,
			validateStack: makeStackFunc(1, 1),
			valid:         true,
		},
		SSTORE: {
			execute:       opSstore,
			gasCost:       gasSStore,
			validateStack: makeStackFunc(2, 0),
			valid:         true,
			writes:        true,
		},
		JUMP: {},
		JUMPI: {
			execute:       opJumpi,
			gasCost:       constGasFunc(GasSlowStep),
			validateStack: makeStackFunc(1, 0),
			jumps:         true,
			valid:         true,
		},
		PC: {},
		MSIZE: {
			execute:       opMsize,
			gasCost:       constGasFunc(GasQuickStep),
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		GAS: {},
		JUMPDEST: {
			execute:       opJumpdest,
			gasCost:       constGasFunc(params.JumpdestGas),
			validateStack: makeStackFunc(0, 0),
			valid:         true,
		},
		PUSH1: {
			execute:       makePush(1, 1),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1), // 压1
			valid:         true,
		},
		PUSH2: {
			execute:       makePush(2, 2),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH3: {
			execute:       makePush(3, 3),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH4: {
			execute:       makePush(4, 4),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH5: {
			execute:       makePush(5, 5),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH6: {
			execute:       makePush(6, 6),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH7: {
			execute:       makePush(7, 7),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH8: {
			execute:       makePush(8, 8),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH9: {
			execute:       makePush(9, 9),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH10: {
			execute:       makePush(10, 10),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH11: {
			execute:       makePush(11, 11),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH12: {
			execute:       makePush(12, 12),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH13: {
			execute:       makePush(13, 13),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH14: {
			execute:       makePush(14, 14),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH15: {
			execute:       makePush(15, 15),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH16: {
			execute:       makePush(16, 16),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH17: {
			execute:       makePush(17, 17),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH18: {
			execute:       makePush(18, 18),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH19: {
			execute:       makePush(19, 19),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH20: {
			execute:       makePush(20, 20),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH21: {
			execute:       makePush(21, 21),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH22: {
			execute:       makePush(22, 22),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH23: {
			execute:       makePush(23, 23),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH24: {
			execute:       makePush(24, 24),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH25: {
			execute:       makePush(25, 25),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH26: {
			execute:       makePush(26, 26),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH27: {
			execute:       makePush(27, 27),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH28: {
			execute:       makePush(28, 28),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH29: {
			execute:       makePush(29, 29),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH30: {
			execute:       makePush(30, 30),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH31: {
			execute:       makePush(31, 31),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		PUSH32: {
			execute:       makePush(32, 32),
			gasCost:       gasPush,
			validateStack: makeStackFunc(0, 1),
			valid:         true,
		},
		DUP1: {
			execute:       makeDup(1),
			gasCost:       gasDup,
			validateStack: makeDupStackFunc(1),
			valid:         true,
		},
		DUP2:   {},
		DUP3:   {},
		DUP4:   {},
		DUP5:   {},
		DUP6:   {},
		DUP7:   {},
		DUP8:   {},
		DUP9:   {},
		DUP10:  {},
		DUP11:  {},
		DUP12:  {},
		DUP13:  {},
		DUP14:  {},
		DUP15:  {},
		DUP16:  {},
		SWAP1:  {},
		SWAP2:  {},
		SWAP3:  {},
		SWAP4:  {},
		SWAP5:  {},
		SWAP6:  {},
		SWAP7:  {},
		SWAP8:  {},
		SWAP9:  {},
		SWAP10: {},
		SWAP11: {},
		SWAP12: {},
		SWAP13: {},
		SWAP14: {},
		SWAP15: {},
		SWAP16: {},
		LOG0:   {},
		LOG1:   {},
		LOG2:   {},
		LOG3:   {},
		LOG4:   {},
		CREATE: {},
		CALL:   {},
		CALLCODE: {
			execute:       opCallCode,
			gasCost:       gasCallCode,
			validateStack: makeStackFunc(7, 1),
			memorySize:    memoryCallCode,
			valid:         true,
			returns:       true,
		},
		RETURN: {
			execute:       opReturn,
			gasCost:       gasReturn,
			validateStack: makeStackFunc(2, 0),
			memorySize:    memoryReturn,
			halts:         true,
			valid:         true,
		},
		SELFDESTRUCT: {
			execute:       opSuicide,
			gasCost:       gasSuicide,
			validateStack: makeStackFunc(1, 0),
			halts:         true,
			valid:         true,
			writes:        true,
		},
	}
}
