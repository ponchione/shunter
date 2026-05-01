package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

var readPolicyFuzzSeeds = [][]byte{
	[]byte(`{"access":"private","permissions":[]}`),
	[]byte(`{"access":"public","permissions":[]}`),
	[]byte(`{"access":"permissioned","permissions":["messages:read"]}`),
	[]byte(`{"access":"permissioned","permissions":[]}`),
	[]byte(`{"access":"public","permissions":["unexpected"]}`),
	[]byte(`{"access":"permissioned","permissions":["messages:read","messages:read"]}`),
	[]byte(`{"access":"permissioned","permissions":[" "]}`),
	[]byte(`{"access":"unknown","permissions":[]}`),
	[]byte(`{"access":1,"permissions":[]}`),
	[]byte(`null`),
	[]byte(`{}`),
	[]byte(`{"access":"private"}`),
	[]byte(`{"permissions":[]}`),
	[]byte(`{"permissions":null}`),
	[]byte(`not-json`),
}

const maxReadPolicyFuzzBytes = 8 << 10

func FuzzReadPolicyJSONRoundTrip(f *testing.F) {
	for _, seed := range readPolicyFuzzSeeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxReadPolicyFuzzBytes {
			t.Skip("read policy JSON fuzz input above bounded local limit")
		}
		assertReadPolicyJSONInput(t, data)
	})
}

func assertReadPolicyJSONInput(tb testing.TB, data []byte) {
	tb.Helper()
	if err := checkReadPolicyJSONInput(data); err != nil {
		tb.Fatal(err)
	}
}

func checkReadPolicyJSONInput(data []byte) error {
	label := readPolicyFuzzLabel(data, maxReadPolicyFuzzBytes)

	var policy ReadPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		if !errors.Is(err, ErrInvalidTableReadPolicy) {
			var syntaxErr *json.SyntaxError
			var typeErr *json.UnmarshalTypeError
			if !errors.As(err, &syntaxErr) && !errors.As(err, &typeErr) {
				return fmt.Errorf("%s operation=UnmarshalReadPolicy observed_error=%v expected_json_or_wrapped=%v", label, err, ErrInvalidTableReadPolicy)
			}
		}
		return nil
	}
	if err := ValidateReadPolicy(policy); err != nil && !errors.Is(err, ErrInvalidTableReadPolicy) {
		return fmt.Errorf("%s operation=ValidateReadPolicy observed_error=%v expected_wrapped=%v", label, err, ErrInvalidTableReadPolicy)
	}

	first, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("%s operation=MarshalReadPolicy observed_error=%v expected=nil", label, err)
	}
	second, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("%s operation=MarshalReadPolicyAgain observed_error=%v expected=nil", label, err)
	}
	if !bytes.Equal(first, second) {
		return fmt.Errorf("%s operation=MarshalDeterminism observed=%s expected=%s", label, second, first)
	}

	var roundTrip ReadPolicy
	if err := json.Unmarshal(first, &roundTrip); err != nil {
		return fmt.Errorf("%s operation=UnmarshalCanonical observed_error=%v expected=nil canonical=%s", label, err, first)
	}
	roundTripCanonical, err := json.Marshal(roundTrip)
	if err != nil {
		return fmt.Errorf("%s operation=MarshalRoundTrip observed_error=%v expected=nil round_trip=%#v", label, err, roundTrip)
	}
	if !bytes.Equal(roundTripCanonical, first) {
		return fmt.Errorf("%s operation=CanonicalRoundTrip observed=%s expected=%s round_trip=%#v", label, roundTripCanonical, first, roundTrip)
	}
	if len(roundTrip.Permissions) == 0 {
		return nil
	}
	roundTrip.Permissions[0] = "mutated"
	var again ReadPolicy
	if err := json.Unmarshal(first, &again); err != nil {
		return fmt.Errorf("%s operation=UnmarshalCanonicalAgain observed_error=%v expected=nil", label, err)
	}
	if again.Permissions[0] == "mutated" {
		return fmt.Errorf("%s operation=PermissionSliceDetachment observed_mutated_permission=true canonical=%s", label, first)
	}
	return nil
}

func readPolicyFuzzLabel(data []byte, maxBytes int) string {
	if len(data) <= 80 {
		return fmt.Sprintf("seed_len=%d seed=%x read_policy_config=max_bytes=%d", len(data), data, maxBytes)
	}
	return fmt.Sprintf("seed_len=%d seed_prefix=%x read_policy_config=max_bytes=%d", len(data), data[:80], maxBytes)
}
