package params

import (
	"fmt"
	"math/big"
)

type ChainConfig struct {
	ChainId        *big.Int `json:"chainId"`
	HomesteadBlock *big.Int `json:"homesteadBlock, omitempty"`
	DAOForkBlock   *big.Int `json:"daoForkBlock, omitempty"`
	DAOForkSupport bool     `json:"daoForkSupport, omitempty"`

	EIP150Block *big.Int `json:"eip150Block, omitempty"`
	EIP150Hash  *big.Int `json:"eip150Hash, omitempty"`

	Ethash *EthashConfig `json:"ethash, omitempty"`
	Clique *CliqueConfig `json:"clique, omitempty"`
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
	return GasTableEIP158
}
