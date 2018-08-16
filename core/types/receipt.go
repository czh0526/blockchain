package types

import (
	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/rlp"
)

type Receipt struct {
	PostState         []byte `json:"root"`
	Status            uint64 `json:"status"`
	CumulativeGasUsed uint64 `json:"cumulativeGasUsed" gencodec:"required"`
	Bloom             Bloom  `json:"logsBloom" gecodec:"required"`
	Logs              []*Log `json:"logs" gencodec:"required"`

	TxHash          common.Hash    `json:"transactionHash" gencodec:"required"`
	ContractAddress common.Address `json:"contractAddress"`
	GasUsed         uint64         `json:"gasUsed" gencodec:"required"`
}

type Receipts []*Receipt

// implements DerivableList interface
func (r Receipts) Len() int {
	return len(r)
}

// implements DerivableList interface
func (r Receipts) GetRlp(i int) []byte {
	bytes, err := rlp.EncodeToBytes(r[i])
	if err != nil {
		panic(err)
	}
	return bytes
}
