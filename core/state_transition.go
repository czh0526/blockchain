package core

import (
	"github.com/czh0526/blockchain/common"
)

type Message interface {
	From() common.Address
	To() *common.Address

	Nonce() uint64
	CheckNonce() bool
	Data() []byte
}
