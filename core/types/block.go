package types

import (
	"math/big"
	"sync/atomic"
	"time"

	"github.com/czh0526/blockchain/common"
)

type Header struct {
	ParentHash  common.Hash    `json:"parentHash" gencodec:"required"`
	UncleHash   common.Hash    `json:"sha3Uncles" gencodec:"required"`
	Coinbase    common.Address `json:"miner" gencodec:"required"`
	Root        common.Hash    `json:"stateRoot" gencodec:"required"`
	TxHash      common.Hash    `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash common.Hash    `json"receiptsRoot" gencodec:"required"`
	Bloom       Bloom          `json:"logsBloom" gencodec:"required"`
	Difficulty  *big.Int       `json:"difficulty" gencodec:"required"`
	Number      *big.Int       `json:"number" gencodec:"required"`
	GasLimit    uint64         `json:"gasLimit" gencodec:"required"`
	GasUsed     uint64         `json:"gasUsed" gencodec:"required"`
	Time        *big.Int       `json:"timestamp" gencodec:"required"`
	Extra       []byte         `json:"extraData" gencodec:"required"`
	MixDigest   common.Hash    `json:"mixHash" gencodec:"required"`
	Nonce       BlockNonce     `json:"nonce" gencodec:"required"`
}

type BlockNonce [8]byte

type Body struct {
	Transactions []*Transaction
	Uncles       []*Header
}

type Block struct {
	header       *Header
	uncles       []*Header
	transactions Transactions

	// caches
	hash atomic.Value
	size atomic.Value

	td           *big.Int
	ReceivedAt   time.Time
	ReceivedFrom interface{}
}
