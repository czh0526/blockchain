package types

const (
	BloomByteLength = 256
	BloomBitLength  = 8 * BloomByteLength
)

type Bloom [BloomByteLength]byte

func CreateBloom(receipts Receipts) Bloom {

}
