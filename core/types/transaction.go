package types

import (
	"math/big"
	"sync/atomic"

	"github.com/czh0526/blockchain/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type Transaction struct {
	data txdata
	// caches
	hash atomic.Value
	size atomic.Value
	from atomic.Value
}

type txdata struct {
	AccountNonce uint64          `json:"nonce" gencodec:"required"`
	Price        *big.Int        `json:"gasPrice" gencodec:"required"`
	GasLimit     uint64          `json:"gas" gencodec:"required"`
	Recipient    *common.Address `json:"gas" gencodec:"required"`
	Amount       *big.Int        `json:"value" gencoded:"required"`
	Payload      []byte          `json:"input" gencodec:"required"`

	V *big.Int `json:"v" gencodec:"required"`
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`

	Hash *common.Hash `json:"hash" rlp:"-"`
}

type txdataMarshaling struct {
	AccountNonce hexutil.Uint64
	Price        *hexutil.Big
	GasLimit     hexutil.Uint64
	Amount       *hexutil.Big
	Payload      hexutil.Bytes
	V            *hexutil.Big
	R            *hexutil.Big
	S            *hexutil.Big
}

type Transactions []*Transaction

