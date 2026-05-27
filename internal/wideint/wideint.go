package wideint

import (
	"encoding/binary"
	"math/big"

	"github.com/ponchione/shunter/types"
)

var (
	uint128Limit = new(big.Int).Lsh(big.NewInt(1), 128)
	uint256Limit = new(big.Int).Lsh(big.NewInt(1), 256)
	int128Max    = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
	int128Min    = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127))
	int256Max    = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1))
	int256Min    = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
)

func IsInt128(n *big.Int) bool {
	return n.Cmp(int128Min) >= 0 && n.Cmp(int128Max) <= 0
}

func IsUint128(n *big.Int) bool {
	return n.Sign() >= 0 && n.Cmp(uint128Limit) < 0
}

func IsInt256(n *big.Int) bool {
	return n.Cmp(int256Min) >= 0 && n.Cmp(int256Max) <= 0
}

func IsUint256(n *big.Int) bool {
	return n.Sign() >= 0 && n.Cmp(uint256Limit) < 0
}

func Int128(n *big.Int) types.Value {
	var buf [16]byte
	fillSigned(buf[:], n, uint128Limit)
	return types.NewInt128(
		int64(binary.BigEndian.Uint64(buf[0:8])),
		binary.BigEndian.Uint64(buf[8:16]),
	)
}

func Uint128(n *big.Int) types.Value {
	var buf [16]byte
	n.FillBytes(buf[:])
	return types.NewUint128(
		binary.BigEndian.Uint64(buf[0:8]),
		binary.BigEndian.Uint64(buf[8:16]),
	)
}

func Int256(n *big.Int) types.Value {
	var buf [32]byte
	fillSigned(buf[:], n, uint256Limit)
	return types.NewInt256(
		int64(binary.BigEndian.Uint64(buf[0:8])),
		binary.BigEndian.Uint64(buf[8:16]),
		binary.BigEndian.Uint64(buf[16:24]),
		binary.BigEndian.Uint64(buf[24:32]),
	)
}

func Uint256(n *big.Int) types.Value {
	var buf [32]byte
	n.FillBytes(buf[:])
	return types.NewUint256(
		binary.BigEndian.Uint64(buf[0:8]),
		binary.BigEndian.Uint64(buf[8:16]),
		binary.BigEndian.Uint64(buf[16:24]),
		binary.BigEndian.Uint64(buf[24:32]),
	)
}

func fillSigned(dst []byte, n, limit *big.Int) {
	if n.Sign() >= 0 {
		n.FillBytes(dst)
		return
	}
	new(big.Int).Add(n, limit).FillBytes(dst)
}
