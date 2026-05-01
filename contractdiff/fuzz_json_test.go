package contractdiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	shunter "github.com/ponchione/shunter"
)

func FuzzCompareAndPlanJSON(f *testing.F) {
	metamorphicOld, metamorphicCurrent := contractOrderMetamorphicFixtures(f)
	validOld := mustFuzzContractJSON(f, contractFixture())
	validCurrent := mustFuzzContractJSON(f, metamorphicCurrent)
	validMetamorphicOld := mustFuzzContractJSON(f, metamorphicOld)

	for _, seed := range []struct {
		old     []byte
		current []byte
	}{
		{old: nil, current: validOld},
		{old: []byte("not-json"), current: validOld},
		{old: validOld, current: []byte(`{`)},
		{old: validOld, current: []byte(`{"contract_version":0}`)},
		{old: validOld, current: validOld},
		{old: validMetamorphicOld, current: validCurrent},
	} {
		f.Add(seed.old, seed.current)
	}

	const maxContractDiffFuzzBytes = 128 << 10
	f.Fuzz(func(t *testing.T, oldData, currentData []byte) {
		if len(oldData)+len(currentData) > maxContractDiffFuzzBytes {
			t.Skip("contractdiff fuzz input above bounded local limit")
		}
		label := contractDiffFuzzLabel(oldData, currentData, maxContractDiffFuzzBytes)

		report, err := CompareJSON(oldData, currentData)
		if err != nil {
			if !errors.Is(err, ErrInvalidContractJSON) {
				t.Fatalf("%s operation=CompareJSON observed_error=%v expected_wrapped=%v", label, err, ErrInvalidContractJSON)
			}
			assertPlanJSONInvalidContractError(t, label, oldData, currentData)
			return
		}

		secondReport, err := CompareJSON(oldData, currentData)
		if err != nil {
			t.Fatalf("%s operation=CompareJSONAgain observed_error=%v expected=nil", label, err)
		}
		if got, want := secondReport.Text(), report.Text(); got != want {
			t.Fatalf("%s operation=CompareJSONDeterminism observed=%s expected=%s", label, got, want)
		}

		var oldContract, currentContract shunter.ModuleContract
		if err := json.Unmarshal(oldData, &oldContract); err != nil {
			t.Fatalf("%s operation=UnmarshalAcceptedOld observed_error=%v expected=nil", label, err)
		}
		if err := json.Unmarshal(currentData, &currentContract); err != nil {
			t.Fatalf("%s operation=UnmarshalAcceptedCurrent observed_error=%v expected=nil", label, err)
		}
		if err := oldContract.Validate(); err != nil {
			t.Fatalf("%s operation=ValidateAcceptedOld observed_error=%v expected=nil", label, err)
		}
		if err := currentContract.Validate(); err != nil {
			t.Fatalf("%s operation=ValidateAcceptedCurrent observed_error=%v expected=nil", label, err)
		}
		if got, want := report.Text(), Compare(oldContract, currentContract).Text(); got != want {
			t.Fatalf("%s operation=CompareJSONModelEquivalence observed=%s expected=%s", label, got, want)
		}

		canonicalOld, err := oldContract.MarshalCanonicalJSON()
		if err != nil {
			t.Fatalf("%s operation=MarshalCanonicalOld observed_error=%v expected=nil", label, err)
		}
		canonicalCurrent, err := currentContract.MarshalCanonicalJSON()
		if err != nil {
			t.Fatalf("%s operation=MarshalCanonicalCurrent observed_error=%v expected=nil", label, err)
		}
		canonicalReport, err := CompareJSON(canonicalOld, canonicalCurrent)
		if err != nil {
			t.Fatalf("%s operation=CompareCanonicalJSON observed_error=%v expected=nil", label, err)
		}
		if got, want := canonicalReport.Text(), report.Text(); got != want {
			t.Fatalf("%s operation=CompareCanonicalEquivalence observed=%s expected=%s", label, got, want)
		}

		plan, err := PlanJSON(oldData, currentData, contractDiffFuzzPlanOptions())
		if err != nil {
			t.Fatalf("%s operation=PlanJSON observed_error=%v expected=nil", label, err)
		}
		firstPlanJSON := mustFuzzPlanJSON(t, label, "MarshalPlanJSON", plan)
		secondPlan, err := PlanJSON(oldData, currentData, contractDiffFuzzPlanOptions())
		if err != nil {
			t.Fatalf("%s operation=PlanJSONAgain observed_error=%v expected=nil", label, err)
		}
		secondPlanJSON := mustFuzzPlanJSON(t, label, "MarshalPlanJSONAgain", secondPlan)
		if !bytes.Equal(firstPlanJSON, secondPlanJSON) {
			t.Fatalf("%s operation=PlanJSONDeterminism observed=%s expected=%s", label, secondPlanJSON, firstPlanJSON)
		}
		canonicalPlan, err := PlanJSON(canonicalOld, canonicalCurrent, contractDiffFuzzPlanOptions())
		if err != nil {
			t.Fatalf("%s operation=PlanCanonicalJSON observed_error=%v expected=nil", label, err)
		}
		canonicalPlanJSON := mustFuzzPlanJSON(t, label, "MarshalCanonicalPlanJSON", canonicalPlan)
		if !bytes.Equal(firstPlanJSON, canonicalPlanJSON) {
			t.Fatalf("%s operation=PlanCanonicalEquivalence observed=%s expected=%s", label, canonicalPlanJSON, firstPlanJSON)
		}
	})
}

func assertPlanJSONInvalidContractError(t *testing.T, label string, oldData, currentData []byte) {
	t.Helper()
	_, err := PlanJSON(oldData, currentData, contractDiffFuzzPlanOptions())
	if err == nil {
		t.Fatalf("%s operation=PlanJSONAfterCompareReject observed_error=nil expected_wrapped=%v", label, ErrInvalidContractJSON)
	}
	if !errors.Is(err, ErrInvalidContractJSON) {
		t.Fatalf("%s operation=PlanJSONAfterCompareReject observed_error=%v expected_wrapped=%v", label, err, ErrInvalidContractJSON)
	}
}

func contractDiffFuzzPlanOptions() PlanOptions {
	return PlanOptions{
		Policy:            PolicyOptions{RequirePreviousVersion: true, Strict: true},
		ValidateContracts: true,
	}
}

func mustFuzzContractJSON(tb testing.TB, contract shunter.ModuleContract) []byte {
	tb.Helper()
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		tb.Fatalf("marshal contractdiff fuzz seed: %v", err)
	}
	return data
}

func mustFuzzPlanJSON(t *testing.T, label, operation string, plan MigrationPlan) []byte {
	t.Helper()
	data, err := plan.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("%s operation=%s observed_error=%v expected=nil", label, operation, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("%s operation=%s observed_trailing_newline=%v expected=true plan=%s",
			label, operation, len(data) > 0 && data[len(data)-1] == '\n', data)
	}
	return data
}

func contractDiffFuzzLabel(oldData, currentData []byte, maxBytes int) string {
	return fmt.Sprintf(
		"old_%s current_%s contractdiff_config=max_total_bytes=%d",
		contractDiffFuzzBytesLabel(oldData),
		contractDiffFuzzBytesLabel(currentData),
		maxBytes,
	)
}

func contractDiffFuzzBytesLabel(data []byte) string {
	if len(data) <= 48 {
		return fmt.Sprintf("len=%d seed=%x", len(data), data)
	}
	return fmt.Sprintf("len=%d seed_prefix=%x", len(data), data[:48])
}
