package params

const (
	QuadCoeffDiv    uint64 = 512
	CallCreateDepth uint64 = 1024
	MemoryGas       uint64 = 3

	CallValueTransferGas uint64 = 9000

	Sha3Gas          uint64 = 30
	Sha3WordGas      uint64 = 6
	SstoreSetGas     uint64 = 20000
	SstoreResetGas   uint64 = 5000
	SstoreClearGas   uint64 = 5000
	SstoreRefundGas  uint64 = 15000
	JumpdestGas      uint64 = 1
	CopyGas          uint64 = 3
	StackLimit       uint64 = 1024
	CreateGas        uint64 = 32000
	SuicideRefundGas uint64 = 24000

	// Precompiled contract gas prices

	EcrecoverGas            uint64 = 3000   // Elliptic curve sender recovery gas price
	Sha256BaseGas           uint64 = 60     // Base price for a SHA256 operation
	Sha256PerWordGas        uint64 = 12     // Per-word price for a SHA256 operation
	Ripemd160BaseGas        uint64 = 600    // Base price for a RIPEMD160 operation
	Ripemd160PerWordGas     uint64 = 120    // Per-word price for a RIPEMD160 operation
	IdentityBaseGas         uint64 = 15     // Base price for a data copy operation
	IdentityPerWordGas      uint64 = 3      // Per-work price for a data copy operation
	ModExpQuadCoeffDiv      uint64 = 20     // Divisor for the quadratic particle of the big int modular exponentiation
	Bn256AddGas             uint64 = 500    // Gas needed for an elliptic curve addition
	Bn256ScalarMulGas       uint64 = 40000  // Gas needed for an elliptic curve scalar multiplication
	Bn256PairingBaseGas     uint64 = 100000 // Base price for an elliptic curve pairing check
	Bn256PairingPerPointGas uint64 = 80000  // Per-point price for an elliptic curve pairing check

)
