package vm

import (
	"math/big"

	"github.com/czh0526/blockchain/common"
)

type StateDB interface {
	CreateAccount(common.Address)

	SubBalance(common.Address, *big.Int)
	AddBalance(common.Address, *big.Int)
	GetBalance(common.Address) *big.Int

	GetCodeHash(common.Address) common.Hash
	GetCode(common.Address) []byte
	SetCode(common.Address, []byte)
	GetCodeSize(common.Address) int

	GetState(common.Address, common.Hash) common.Hash
	SetState(common.Address, common.Hash, common.Hash)

	AddRefund(uint64)
	GetRefund() uint64

	Exist(common.Address) bool
	Empty(common.Address) bool

	Snapshot() int
	RevertToSnapshot(int)

	AddPreimage(common.Hash, []byte)

	Suicide(common.Address) bool
	HasSuicided(common.Address) bool 
}
