package params

import (
	"fmt"
	"math/big"

	"github.com/czh0526/blockchain/common"
)

var (
	TestChainConfig = &ChainConfig{
		big.NewInt(1),     // ChainId
		big.NewInt(0),     // HomesteadBlock
		nil,               // DAOForkBlock
		false,             // DAOForkSupport
		big.NewInt(0),     // EIP150Block
		common.Hash{},     // EIP150Hash
		big.NewInt(0),     // EIP155Block
		big.NewInt(0),     // EIP158Block
		big.NewInt(0),     // ByzantiumBlock
		nil,               // ConstantinopleBlock
		new(EthashConfig), // EthashConfig
		nil,               // Clique
	}
)

type ChainConfig struct {
	ChainId        *big.Int `json:"chainId"`
	HomesteadBlock *big.Int `json:"homesteadBlock,omitempty"`
	DAOForkBlock   *big.Int `json:"daoForkBlock,omitempty"`
	DAOForkSupport bool     `json:"daoForkSupport,omitempty"`

	EIP150Block *big.Int    `json:"eip150Block,omitempty"`
	EIP150Hash  common.Hash `json:"eip150Hash,omitempty"`

	EIP155Block *big.Int `json:"eip155Block,omitempty"`
	EIP158Block *big.Int `json:"eip158Block,omitempty"`

	ByzantiumBlock      *big.Int `json:"byzantiumBlock,omitempty"`
	ConstantinopleBlock *big.Int `json:"constantinopBlock,omitempty"`

	Ethash *EthashConfig `json:"ethash,omitempty"`
	Clique *CliqueConfig `json:"clique,omitempty"`
}

func (c *ChainConfig) String() string {
	var engine interface{}

	switch {
	case c.Ethash != nil:
		engine = c.Ethash
	case c.Clique != nil:
		engine = c.Clique
	default:
		engine = "unknown"
	}

	return fmt.Sprintf("{ChainID: %v, Engine: %v}", c.ChainId, engine)
}

func (c *ChainConfig) GasTable(num *big.Int) GasTable {
	if num == nil {
		return GasTableHomestead
	}
	switch {
	default:
		return GasTableHomestead
	}
}

type EthashConfig struct{}

func (c *EthashConfig) String() string {
	return "ethash"
}

type CliqueConfig struct {
	Period uint64 `json:"period"`
	Epoch  uint64 `json:"epoch"`
}

func (c *CliqueConfig) String() string {
	return "clique"
}
