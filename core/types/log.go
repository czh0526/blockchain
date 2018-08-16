package types

import (
	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/common/hexutil"
)

type Log struct {
	Address     common.Address `json:"address" gencodec:"required"`
	Topics      []common.Hash  `json:"topics" gencodec:"required"`
	Data        []byte         `json:"data" gencodec:"required"`
	BlockNumber uint64         `json:"blockNumber"`
	TxHash      common.Hash    `json:"transactionHash" gencodec:"required"`
	TxIndex     uint           `json:"transactionIndex" gencodec:"required"`
	BlockHash   common.Hash    `json:"blockHash"`
	Index       uint           `json:"logIndex" gencodec:"required"`
	Removed     bool           `json:"removed"`
}

type logMarshaling struct {
	Data        hexutil.Bytes
	BlockNumber hexutil.Uint64
	TxIndex     hexutil.Uint
	Index       hexutil.Uint
}
