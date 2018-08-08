package vm

import (
	"math/big"

	"github.com/czh0526/blockchain/common"
)

type ContractRef interface {
	Address() common.Address
}

type AccountRef common.Address

func (ar AccountRef) Address() common.Address {
	return (common.Address)(ar)
}

type Contract struct {
	CallerAddress common.Address
	caller        ContractRef
	self          ContractRef

	jumpdests destinations

	Code     []byte
	CodeHash common.Hash
	CodeAddr *common.Address // 预编译合约的地址
	Input    []byte

	Gas   uint64
	value *big.Int

	Args         []byte
	DelegateCall bool
}

func NewContract(caller ContractRef, object ContractRef, value *big.Int, gas uint64) *Contract {
	c := &Contract{
		CallerAddress: caller.Address(),
		caller:        caller,
		self:          object,
		Args:          nil,
	}

	if parent, ok := caller.(*Contract); ok {
		c.jumpdests = parent.jumpdests
	} else {
		c.jumpdests = make(destinations)
	}

	c.Gas = gas
	c.value = value

	return c
}

func (c *Contract) Address() common.Address {
	return c.self.Address()
}

func (c *Contract) SetCallCode(addr *common.Address, hash common.Hash, code []byte) {
	c.Code = code
	c.CodeHash = hash
	c.CodeAddr = addr
}

// 获取位置 n 的 opcode
func (c *Contract) GetOp(n uint64) OpCode {
	return OpCode(c.GetByte(n))
}

// 在 code 字节中获取指定位置的字节内容
func (c *Contract) GetByte(n uint64) byte {
	if n < uint64(len(c.Code)) {
		return c.Code[n]
	}
	return 0
}

func (c *Contract) UseGas(gas uint64) (ok bool) {
	if c.Gas < gas {
		return false
	}
	c.Gas -= gas
	return true
}
