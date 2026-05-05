package types

import (
	"errors"
	"strings"
	"testing"
)

func TestFixedHexParsersMetamorphicCorpus(t *testing.T) {
	const seed = uint64(0x4e51d7)
	for caseIndex, tc := range fixedHexParserCases {
		variants := []string{
			tc.canonical,
			strings.ToUpper(tc.canonical),
			mixedHexCase(tc.canonical),
		}
		for variantIndex, variant := range variants {
			opIndex := caseIndex*20 + variantIndex
			got, err := tc.parse(variant)
			if err != nil {
				t.Fatalf("seed=%#x op_index=%d runtime_config=parser=%s operation=parse-valid input=%q observed_error=%v expected=nil",
					seed, opIndex, tc.name, variant, err)
			}
			if got != tc.canonical {
				t.Fatalf("seed=%#x op_index=%d runtime_config=parser=%s operation=parse-valid input=%q observed=%q expected=%q",
					seed, opIndex, tc.name, variant, got, tc.canonical)
			}
		}
		for invalidIndex, input := range tc.invalid {
			opIndex := caseIndex*20 + len(variants) + invalidIndex
			got, err := tc.parse(input)
			if !errors.Is(err, tc.invalidErr) {
				t.Fatalf("seed=%#x op_index=%d runtime_config=parser=%s operation=parse-invalid input=%q observed=(value=%q,error=%v) expected_error=%v",
					seed, opIndex, tc.name, input, got, err, tc.invalidErr)
			}
		}
	}
}

func mixedHexCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		if i%3 == 0 {
			b.WriteRune(r)
			continue
		}
		b.WriteString(strings.ToUpper(string(r)))
	}
	return b.String()
}
