package tests

import (
	"encoding/json"
	"math/big"

	"github.com/czh0526/blockchain/core"
	"github.com/czh0526/blockchain/crypto"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/common/hexutil"
	"github.com/czh0526/blockchain/common/math"
	"github.com/czh0526/blockchain/core/state"
	"github.com/czh0526/blockchain/core/vm"
	"github.com/czh0526/blockchain/ethdb"
)

type VMTest struct {
	json vmJSON
}

func (t *VMTest) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &t.json)
}

type vmJSON struct {
	Env           stEnv                 `json:"env"`
	Exec          vmExec                `json:"exec"`
	Logs          common.UnprefixedHash `json:"logs"`
	GasRemaining  *math.HexOrDecimal64  `json:"gas"`
	Out           hexutil.Bytes         `json:"out"`
	Pre           core.GenesisAlloc     `json:"pre"`
	Post          core.GenesisAlloc     `json:"post"`
	PostStateRoot common.Hash           `json:"postStateRoot"`
}

type vmExec struct {
	Address  common.Address `json:"address" gencodec:"required"`
	Caller   common.Address `json:"caller" gencodec:"required"`
	Origin   common.Address `json:"origin" gencodec:"required"`
	Code     []byte         `json:"code" gencodec:"required"`
	Data     []byte         `json:"data" gencodec:"required"`
	Value    *big.Int       `json:"value" gencodec:"required"`
	GasLimit uint64         `json:"gas" gencodec:"required"`
	GasPrice *big.Int       `json:"gasprice" gencodec:"required"`
}

func (t *VMTest) Run(vmconfig vm.Config) error {
	statedb := MakePreState(ethdb.NewMemDatabase(), t.json.Pre)
	ret, gasRemaining, err := t.exec(statedb, vmconfig)

}

func (t *VMTest) exec(statedb *state.StateDB, vmconfig vm.Config) ([]byte, uint64, error) {
	evm := t.newEVM(statedb, vmconfig)
	e := t.json.Exec
	return evm.Call(vm.AccountRef(e.Caller), e.Address, e.Data, e.GasLimit, e.Value)
}

func (t *VMTest) newEVM(statedb *state.StateDB, vmconfig vm.Config) *vm.EVM {
	initialCall := true
	canTransfer := func(db vm.StateDB, address common.Address, amount *big.Int) bool {
		if initialCall {
			initialCall = false
			return true
		}
		return core.CanTransfer(db, address, amount)
	}
	transfer := func(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {}
	context := vm.Context{
		CanTransfer: canTransfer,
		Transfer:    transfer,
		GetHash:     vmTestBlockHash,
	}
	vmconfig.NoRecursion = true
	return vm.NewEVM(context, statedb)
}

func vmTestBlockHash(n uint64) common.Hash {
	return common.BytesToHash(
		crypto.Keccak256(
			[]byte(
				big.NewInt(int64(n)).String())))
}
