package types

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const (
	fixedHexIdentitySeed     = "00112233445566778899aabbccddeeffffeeddccbbaa99887766554433221100"
	fixedHexConnectionIDSeed = "00112233445566778899aabbccddeeff"
)

type fixedHexParserCase struct {
	name       string
	canonical  string
	wantLen    int
	invalidErr error
	invalid    []string
	parse      func(string) (string, error)
}

var fixedHexParserCases = []fixedHexParserCase{
	{
		name:       "identity",
		canonical:  fixedHexIdentitySeed,
		wantLen:    64,
		invalidErr: ErrInvalidIdentityHex,
		invalid: []string{
			"",
			"ab",
			"0123",
			fixedHexIdentitySeed[:63],
			fixedHexIdentitySeed + "0",
			fixedHexIdentitySeed[:63] + "z",
			" " + fixedHexIdentitySeed[:63],
			"0x" + fixedHexIdentitySeed[:62],
		},
		parse: func(s string) (string, error) {
			id, err := ParseIdentityHex(s)
			if err != nil {
				return "", err
			}
			return id.Hex(), nil
		},
	},
	{
		name:       "connection_id",
		canonical:  fixedHexConnectionIDSeed,
		wantLen:    32,
		invalidErr: ErrInvalidConnectionIDHex,
		invalid: []string{
			"",
			"ab",
			"0123",
			"0123456789abcdef",
			fixedHexConnectionIDSeed[:31],
			fixedHexConnectionIDSeed + "0",
			fixedHexConnectionIDSeed[:31] + "z",
			" " + fixedHexConnectionIDSeed[:31],
			"0x" + fixedHexConnectionIDSeed[:30],
		},
		parse: func(s string) (string, error) {
			connID, err := ParseConnectionIDHex(s)
			if err != nil {
				return "", err
			}
			return connID.Hex(), nil
		},
	},
}

func FuzzParseFixedHexIDs(f *testing.F) {
	for _, seed := range fixedHexParserFuzzSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		for _, tc := range fixedHexParserCases {
			if err := checkFixedHexParserBoundary(tc, input); err != nil {
				t.Fatalf("parser=%s input_len=%d failure=%v", tc.name, len(input), err)
			}
		}
	})
}

func fixedHexParserFuzzSeeds() []string {
	seeds := []string{" ", "\x00"}
	for _, tc := range fixedHexParserCases {
		seeds = append(seeds, tc.canonical, strings.ToUpper(tc.canonical), mixedHexCase(tc.canonical))
		seeds = append(seeds, tc.invalid...)
	}
	return seeds
}

func checkFixedHexParserBoundary(tc fixedHexParserCase, input string) error {
	got, err := tc.parse(input)
	if err != nil {
		if !errors.Is(err, tc.invalidErr) {
			return fmt.Errorf("operation=parse observed_error=%v expected_error=%v", err, tc.invalidErr)
		}
		return nil
	}
	if len(input) != tc.wantLen {
		return fmt.Errorf("operation=parse accepted_input_len=%d expected_len=%d", len(input), tc.wantLen)
	}
	if len(got) != tc.wantLen {
		return fmt.Errorf("operation=Hex observed_len=%d expected_len=%d observed=%q", len(got), tc.wantLen, got)
	}
	if got != strings.ToLower(got) {
		return fmt.Errorf("operation=Hex observed=%q expected_lowercase=true", got)
	}
	if err := checkFixedHexVariant(tc, got, got, "canonical"); err != nil {
		return err
	}
	if err := checkFixedHexVariant(tc, strings.ToUpper(got), got, "upper"); err != nil {
		return err
	}
	if err := checkFixedHexVariant(tc, mixedHexCase(got), got, "mixed"); err != nil {
		return err
	}
	return nil
}

func checkFixedHexVariant(tc fixedHexParserCase, input, want, variant string) error {
	got, err := tc.parse(input)
	if err != nil {
		return fmt.Errorf("operation=parse-%s observed_error=%v expected=nil", variant, err)
	}
	if got != want {
		return fmt.Errorf("operation=parse-%s observed=%q expected=%q", variant, got, want)
	}
	return nil
}
