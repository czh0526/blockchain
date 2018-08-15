package vm

import (
	"math/big"
	"time"

	"github.com/czh0526/blockchain/crypto"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/params"
)

var emptyCodeHash = crypto.Keccak256Hash(nil)

type (
	CanTransferFunc func(StateDB, common.Address, *big.Int) bool
	TransferFunc    func(StateDB, common.Address, common.Address, *big.Int)
	GetHashFunc     func(uint64) common.Hash
)

type Context struct {
	CanTransfer CanTransferFunc
	Transfer    TransferFunc
	GetHash     GetHashFunc
	// 消息信息
	Origin   common.Address
	GasPrice *big.Int
	// 区块信息
	Coinbase    common.Address
	GasLimit    uint64
	BlockNumber *big.Int
	Time        *big.Int
	Difficulty  *big.Int
}

type EVM struct {
	Context
	StateDB StateDB
	depth   int

	chainConfig *params.ChainConfig

	vmConfig    Config
	interpreter *Interpreter
	abort       int32

	callGasTemp uint64
}

func NewEVM(ctx Context, statedb StateDB, chainConfig *params.ChainConfig, vmConfig Config) *EVM {
	evm := &EVM{
		Context:     ctx,
		StateDB:     statedb,
		vmConfig:    vmConfig,
		chainConfig: chainConfig,
	}
	evm.interpreter = NewInterpreter(evm, vmConfig)
	return evm
}

func (evm *EVM) ChainConfig() *params.ChainConfig {
	return evm.chainConfig
}

// 执行在 addr 位置的合约代码
func (evm *EVM) Call(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	// 检查支付账户的余额
	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, ErrInsufficientBalance
	}

	var (
		// 被调用人与调用人不一致
		to       = AccountRef(addr)
		snapshot = evm.StateDB.Snapshot()
	)
	if !evm.StateDB.Exist(addr) {
		evm.StateDB.CreateAccount(addr)
	}
	// 支付账户 ==> 目标账户
	evm.Transfer(evm.StateDB, caller.Address(), to.Address(), value)

	// to 设置成 addr
	// code 设置成 addr 对应的合约地址
	contract := NewContract(caller, to, value, gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	start := time.Now()

	if evm.vmConfig.Debug && evm.depth == 0 {
		evm.vmConfig.Tracer.CaptureStart(caller.Address(), addr, false, input, gas, value)

		defer func() {
			evm.vmConfig.Tracer.CaptureEnd(ret, gas-contract.Gas, time.Since(start), err)
		}()
	}
	ret, err = run(evm, contract, input)

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	return ret, contract.Gas, err
}

func (evm *EVM) CallCode(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverFas uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	if !evm.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, ErrInsufficientBalance
	}

	var (
		snapshot = evm.StateDB.Snapshot()
		// 调用人与被调用人一致
		to = AccountRef(caller.Address())
	)

	// to 设置成 caller
	// code 设置成 addr 对应的合约地址
	contract := NewContract(caller, to, value, gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	ret, err = run(evm, contract, input)
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	return ret, contract.Gas, err
}

func (evm *EVM) StaticCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	if !evm.interpreter.readOnly {
		evm.interpreter.readOnly = true
		defer func() {
			evm.interpreter.readOnly = false
		}()
	}

	var (
		to       = AccountRef(addr)
		snapshot = evm.StateDB.Snapshot()
	)

	// 构建合约对象
	contract := NewContract(caller, to, new(big.Int), gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	// 使用 evm 执行合约，input 为参数
	ret, err = run(evm, contract, input)
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}

	return ret, contract.Gas, err
}

// 使用 input 作为参数运行 contract 智能合约
func run(evm *EVM, contract *Contract, input []byte) ([]byte, error) {
	if contract.CodeAddr != nil {
		// 如果是预编译的 contract, 执行预编译的函数
		precompiles := PrecompiledContractsHomestead
		if p := precompiles[*contract.CodeAddr]; p != nil {
			return RunPrecompiledContract(p, input, contract)
		}
	}
	// 否则，运行 contract.Code
	return evm.interpreter.Run(contract, input)
}
