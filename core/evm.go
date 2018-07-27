package core

import (
	"math/big"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/core/vm"
)

func NewEVMContext(msg Message, author *common.Address) vm.Context {

	return vm.Context{
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		GetHash:     nil,
	}
}

// 判断账户中是否有足够的余额进行转出
func CanTransfer(db vm.StateDB, addr common.Address, amount *big.Int) bool {
	return db.GetBalance(addr).Cmp(amount) >= 0
}

// 在两个账户间进行转账
func Transfer(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
