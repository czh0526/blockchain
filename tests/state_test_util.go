package tests

import (
	"math/big"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/common/math"
	"github.com/czh0526/blockchain/core"
	"github.com/czh0526/blockchain/core/state"
	"github.com/czh0526/blockchain/ethdb"
)

type stEnv struct {
	Coinbase   common.Address `json:"currentCoinbase" gencodec:"required"`
	Difficulty *big.Int       `json:"currentDifficluty" gencodec:"required"`
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
