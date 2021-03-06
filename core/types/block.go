package types

import (
	"encoding/binary"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/czh0526/blockchain/crypto/sha3"
	"github.com/czh0526/blockchain/rlp"

	"github.com/czh0526/blockchain/common"
)

var (
	EmptyRootHash  = DeriveSha(Transactions{})
	EmptyUncleHash = CalcUncleHash(nil)
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

func EncodeNonce(i uint64) BlockNonce {
	var n BlockNonce
	binary.BigEndian.PutUint64(n[:], i)
	return n
}

func NewBlock(header *Header, txs []*Transaction, uncles []*Header, receipts []*Receipt) *Block {
	b := &Block{header: CopyHeader(header), td: new(big.Int)}

	// 设置 txs
	if len(txs) == 0 {
		b.header.TxHash = EmptyRootHash
	} else {
		b.header.TxHash = DeriveSha(Transactions(txs))
		b.transactions = make(Transactions, len(txs))
		copy(b.transactions, txs)
	}

	// 设置 receipts
	if len(receipts) == 0 {
		b.header.ReceiptHash = EmptyRootHash
	} else {
		b.header.ReceiptHash = DeriveSha(Receipts(receipts))
		b.header.Bloom = CreateBloom(receipts)
	}

	// 设置 uncles
	if len(uncles) == 0 {
		b.header.UncleHash = EmptyUncleHash
	} else {
		b.header.UncleHash = CalcUncleHash(uncles)
		b.uncles = make([]*Header, len(uncles))
		for i := range uncles {
			b.uncles[i] = CopyHeader(uncles[i])
		}
	}

	return b
}

func CopyHeader(h *Header) *Header {
	// 复制数据
	cpy := *h

	// 重新构建指针成员
	if cpy.Time = new(big.Int); h.Time != nil {
		cpy.Time.Set(h.Time)
	}
	if cpy.Difficulty = new(big.Int); h.Difficulty != nil {
		cpy.Difficulty.Set(h.Difficulty)
	}
	if cpy.Number = new(big.Int); h.Number != nil {
		cpy.Number.Set(h.Number)
	}
	// 重新构建 slice 成员
	if len(h.Extra) > 0 {
		cpy.Extra = make([]byte, len(h.Extra))
		copy(cpy.Extra, h.Extra)
	}
	return &cpy
}

func CalcUncleHash(uncles []*Header) common.Hash {
	return rlpHash(uncles)
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}
