package types

import (
	"encoding/hex"
	"fmt"
)

func isZeroBytes(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func hexString(b []byte) string {
	return hex.EncodeToString(b)
}

func parseFixedHex(s string, want int, baseErr error) ([]byte, error) {
	if len(s) != want {
		return nil, fmt.Errorf("%w: length %d, want %d", baseErr, len(s), want)
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", baseErr, err)
	}
	return b, nil
}
