package core

import (
	"math/big"

	"github.com/czh0526/blockchain/common"
)

type GenesisAccount struct {
	Code       []byte                      `json:"code,omitempty"`
	Storage    map[common.Hash]common.Hash `json:"storage, omitempty"`
	Balance    *big.Int                    `json:"balance" gencodec:"required"`
	Nonce      uint64                      `json:"nonce, omitempty"`
	PrivateKey []byte                      `json:"secretKey, omitempty"`
}

type GenesisAlloc map[common.Address]GenesisAccount
