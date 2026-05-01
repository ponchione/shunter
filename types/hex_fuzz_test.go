package types

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const (
	fixedHexIdentitySeed     = "00112233445566778899aabbccddeeffffeeddccbbaa99887766554433221100"
	fixedHexConnectionIDSeed = "00112233445566778899aabbccddeeff"
)

type fixedHexParserCase struct {
	name       string
	wantLen    int
	invalidErr error
	parse      func(string) (string, error)
}

var fixedHexParserCases = []fixedHexParserCase{
	{
		name:       "identity",
		wantLen:    64,
		invalidErr: ErrInvalidIdentityHex,
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
		wantLen:    32,
		invalidErr: ErrInvalidConnectionIDHex,
		parse: func(s string) (string, error) {
			connID, err := ParseConnectionIDHex(s)
			if err != nil {
				return "", err
			}
			return connID.Hex(), nil
		},
	},
}

var fixedHexParserFuzzSeeds = []string{
	"",
	" ",
	"\x00",
	fixedHexIdentitySeed,
	strings.ToUpper(fixedHexIdentitySeed),
	mixedHexCase(fixedHexIdentitySeed),
	fixedHexIdentitySeed[:63],
	fixedHexIdentitySeed + "0",
	fixedHexIdentitySeed[:63] + "z",
	" " + fixedHexIdentitySeed[:63],
	"0x" + fixedHexIdentitySeed[:62],
	fixedHexConnectionIDSeed,
	strings.ToUpper(fixedHexConnectionIDSeed),
	mixedHexCase(fixedHexConnectionIDSeed),
	fixedHexConnectionIDSeed[:31],
	fixedHexConnectionIDSeed + "0",
	fixedHexConnectionIDSeed[:31] + "z",
	" " + fixedHexConnectionIDSeed[:31],
	"0x" + fixedHexConnectionIDSeed[:30],
}

func FuzzParseFixedHexIDs(f *testing.F) {
	for _, seed := range fixedHexParserFuzzSeeds {
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

func TestFixedHexParserConcurrentShortSoak(t *testing.T) {
	const (
		seed       = uint64(0x1d5eed5)
		workers    = 6
		iterations = 128
	)

	start := make(chan struct{})
	failures := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				parserIndex := (int(seed) + worker*11 + op*7) % len(fixedHexParserCases)
				inputIndex := (int(seed) + worker*17 + op*5) % len(fixedHexParserFuzzSeeds)
				tc := fixedHexParserCases[parserIndex]
				input := fixedHexParserFuzzSeeds[inputIndex]

				if err := checkFixedHexParserBoundary(tc, input); err != nil {
					select {
					case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d parser=%s input=%q failure=%v",
						seed, worker, op, workers, iterations, tc.name, input, err):
					default:
					}
					return
				}
				if (int(seed)+worker+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}

	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
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
