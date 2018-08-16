package tests

import (
	"encoding/json"
	"math/big"

	"github.com/czh0526/blockchain/params"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/common/hexutil"
	"github.com/czh0526/blockchain/common/math"
	"github.com/czh0526/blockchain/core"
	"github.com/czh0526/blockchain/core/state"
	"github.com/czh0526/blockchain/core/vm"
	"github.com/czh0526/blockchain/ethdb"
)

type StateTest struct {
	json stJSON
}

func (t *StateTest) UnmarshalJSON(in []byte) error {
	return json.Unmarshal(in, &t.json)
}

type stJSON struct {
	Env  stEnv                    `json:"env"`
	Pre  core.GenesisAlloc        `josn:"pre"`
	Tx   stTransaction            `json:"transaction"`
	Out  hexutil.Bytes            `json:"out"`
	Post map[string][]stPostState `json:"post"`
}

type stPostState struct {
	Root    common.UnprefixedHash `json:"hash"`
	Logs    common.UnprefixedHash `json:"logs"`
	Indexes struct {
		Data  int `json:"data"`
		Gas   int `json:"gas"`
		Value int `json:"value"`
	}
}

type stEnv struct {
	Coinbase   common.Address `json:"currentCoinbase" gencodec:"required"`
	Difficulty *big.Int       `json:"currentDifficulty" gencodec:"required"`
	GasLimit   uint64         `json:"currentGasLimit" gencodec:"required"`
	Number     uint64         `json:"currentNumber" gencodec:"required"`
	Timestamp  uint64         `json:"currentTimestamp" gencodec:"required"`
}

type stEnvMarshaling struct {
	Coinbase   common.UnprefixedAddress
	Difficulty *math.HexOrDecimal256
	GasLimit   math.HexOrDecimal64
	Number     math.HexOrDecimal64
	Timestamp  math.HexOrDecimal64
}

type stTransaction struct {
	GasPrice   *big.Int `json:"gasPrice"`
	Nonce      uint64   `json:"nonce"`
	To         string   `json:"to"`
	Data       []string `json:"data"`
	GasLimit   []uint64 `json:"gasLimit"`
	Value      []string `json:"value"`
	PrivateKey []byte   `json:"scretKey"`
}

type stTransactionMarshaling struct {
	GasPrice   *math.HexOrDecimal256
	Nonce      math.HexOrDecimal64
	GasLimit   []math.HexOrDecimal64
	PrivateKey hexutil.Bytes
}

func MakePreState(db ethdb.Database, accounts core.GenesisAlloc) *state.StateDB {
	sdb := state.NewDatabase(db)
	statedb, _ := state.New(common.Hash{}, sdb)
	for addr, a := range accounts {
		statedb.SetCode(addr, a.Code)
		statedb.SetNonce(addr, a.Nonce)
		statedb.SetBalance(addr, a.Balance)
		for k, v := range a.Storage {
			statedb.SetState(addr, k, v)
		}
	}

	root, _ := statedb.Commit(false)
	statedb, _ = state.New(root, sdb)
	return statedb
}

type StateSubtest struct {
	Fork  string
	Index int
}

func (t *StateTest) Subtests() []StateSubtest {
	var sub []StateSubtest
	for fork, pss := range t.json.Post {
		for i := range pss {
			sub = append(sub, StateSubtest{fork, i})
		}
	}
	return sub
}

func (t *StateTest) genesis(config *params.ChainConfig) *core.Genesis {
	return &core.Genesis{
		Config:     config,
		Coinbase:   t.json.Env.Coinbase,
		Difficulty: t.json.Env.Difficulty,
		GasLimit:   t.json.Env.GasLimit,
		Number:     t.json.Env.Number,
		Timestamp:  t.json.Env.Timestamp,
		Alloc:      t.json.Pre,
	}
}

func (t *StateTest) Run(subtest StateSubtest, vmconfig vm.Config) (*state.StateDB, error) {
	config, ok := Forks[subtest.Fork]
	if !ok {
		return nil, UnsupportedForkError{subtest.Fork}
	}
	block := t.genesis(config).ToBlock(nil)
	statedb := MakePreState(ethdb.NewMemDatabase(), t.json.Pre)

	post := t.json.Post[subtest.Fork][subtest.Index]

}
