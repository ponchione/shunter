package types

import (
	"encoding/hex"
	"fmt"
)

func hexString(b []byte) string {
	return hex.EncodeToString(b)
}

func parseFixedHex(dst []byte, s string, baseErr error) error {
	want := hex.EncodedLen(len(dst))
	if len(s) != want {
		return fmt.Errorf("%w: length %d, want %d", baseErr, len(s), want)
	}
	if _, err := hex.Decode(dst, []byte(s)); err != nil {
		clear(dst)
		return fmt.Errorf("%w: %v", baseErr, err)
	}
	return nil
}
