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

	Exist(common.Address) bool
	Empty(common.Address) bool

	Snapshot() int
	RevertToSnapshot(int)
}
