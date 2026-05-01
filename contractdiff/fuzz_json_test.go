package contractdiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	shunter "github.com/ponchione/shunter"
)

const maxContractDiffFuzzBytes = 128 << 10

type contractDiffJSONSeed struct {
	old     []byte
	current []byte
}

func FuzzCompareAndPlanJSON(f *testing.F) {
	for _, seed := range contractDiffJSONFuzzSeeds(f) {
		f.Add(seed.old, seed.current)
	}

	f.Fuzz(func(t *testing.T, oldData, currentData []byte) {
		if len(oldData)+len(currentData) > maxContractDiffFuzzBytes {
			t.Skip("contractdiff fuzz input above bounded local limit")
		}
		assertContractDiffJSONInput(t, oldData, currentData)
	})
}

func contractDiffJSONFuzzSeeds(tb testing.TB) []contractDiffJSONSeed {
	tb.Helper()
	metamorphicOld, metamorphicCurrent := contractOrderMetamorphicFixtures(tb)
	validOld := mustFuzzContractJSON(tb, contractFixture())
	validCurrent := mustFuzzContractJSON(tb, metamorphicCurrent)
	validMetamorphicOld := mustFuzzContractJSON(tb, metamorphicOld)

	return []contractDiffJSONSeed{
		{old: nil, current: validOld},
		{old: []byte("not-json"), current: validOld},
		{old: validOld, current: []byte(`{`)},
		{old: validOld, current: []byte(`{"contract_version":0}`)},
		{old: validOld, current: validOld},
		{old: validMetamorphicOld, current: validCurrent},
	}
}

func assertContractDiffJSONInput(tb testing.TB, oldData, currentData []byte) {
	tb.Helper()
	if err := checkContractDiffJSONInput(oldData, currentData); err != nil {
		tb.Fatal(err)
	}
}

func checkContractDiffJSONInput(oldData, currentData []byte) error {
	label := contractDiffFuzzLabel(oldData, currentData, maxContractDiffFuzzBytes)

	report, err := CompareJSON(oldData, currentData)
	if err != nil {
		if !errors.Is(err, ErrInvalidContractJSON) {
			return fmt.Errorf("%s operation=CompareJSON observed_error=%v expected_wrapped=%v", label, err, ErrInvalidContractJSON)
		}
		return checkPlanJSONInvalidContractError(label, oldData, currentData)
	}

	secondReport, err := CompareJSON(oldData, currentData)
	if err != nil {
		return fmt.Errorf("%s operation=CompareJSONAgain observed_error=%v expected=nil", label, err)
	}
	if got, want := secondReport.Text(), report.Text(); got != want {
		return fmt.Errorf("%s operation=CompareJSONDeterminism observed=%s expected=%s", label, got, want)
	}

	var oldContract, currentContract shunter.ModuleContract
	if err := json.Unmarshal(oldData, &oldContract); err != nil {
		return fmt.Errorf("%s operation=UnmarshalAcceptedOld observed_error=%v expected=nil", label, err)
	}
	if err := json.Unmarshal(currentData, &currentContract); err != nil {
		return fmt.Errorf("%s operation=UnmarshalAcceptedCurrent observed_error=%v expected=nil", label, err)
	}
	if err := oldContract.Validate(); err != nil {
		return fmt.Errorf("%s operation=ValidateAcceptedOld observed_error=%v expected=nil", label, err)
	}
	if err := currentContract.Validate(); err != nil {
		return fmt.Errorf("%s operation=ValidateAcceptedCurrent observed_error=%v expected=nil", label, err)
	}
	if got, want := report.Text(), Compare(oldContract, currentContract).Text(); got != want {
		return fmt.Errorf("%s operation=CompareJSONModelEquivalence observed=%s expected=%s", label, got, want)
	}

	canonicalOld, err := oldContract.MarshalCanonicalJSON()
	if err != nil {
		return fmt.Errorf("%s operation=MarshalCanonicalOld observed_error=%v expected=nil", label, err)
	}
	canonicalCurrent, err := currentContract.MarshalCanonicalJSON()
	if err != nil {
		return fmt.Errorf("%s operation=MarshalCanonicalCurrent observed_error=%v expected=nil", label, err)
	}
	canonicalReport, err := CompareJSON(canonicalOld, canonicalCurrent)
	if err != nil {
		return fmt.Errorf("%s operation=CompareCanonicalJSON observed_error=%v expected=nil", label, err)
	}
	if got, want := canonicalReport.Text(), report.Text(); got != want {
		return fmt.Errorf("%s operation=CompareCanonicalEquivalence observed=%s expected=%s", label, got, want)
	}

	policyOpts := contractDiffFuzzPlanOptions().Policy
	firstPolicy := CheckPolicy(report, currentContract, policyOpts)
	if err := checkFuzzPolicyResult(label, "PolicyStrictFailure", firstPolicy, policyOpts); err != nil {
		return err
	}
	secondPolicy := CheckPolicy(secondReport, currentContract, policyOpts)
	if err := checkFuzzPolicyEquivalence(label, "PolicyDeterminism", secondPolicy, firstPolicy); err != nil {
		return err
	}
	modelPolicy := CheckPolicy(Compare(oldContract, currentContract), currentContract, policyOpts)
	if err := checkFuzzPolicyEquivalence(label, "PolicyModelEquivalence", modelPolicy, firstPolicy); err != nil {
		return err
	}
	canonicalPolicy := CheckPolicy(canonicalReport, currentContract, policyOpts)
	if err := checkFuzzPolicyEquivalence(label, "PolicyCanonicalEquivalence", canonicalPolicy, firstPolicy); err != nil {
		return err
	}

	plan, err := PlanJSON(oldData, currentData, contractDiffFuzzPlanOptions())
	if err != nil {
		return fmt.Errorf("%s operation=PlanJSON observed_error=%v expected=nil", label, err)
	}
	firstPlanJSON, err := checkFuzzPlanJSON(label, "MarshalPlanJSON", plan)
	if err != nil {
		return err
	}
	secondPlan, err := PlanJSON(oldData, currentData, contractDiffFuzzPlanOptions())
	if err != nil {
		return fmt.Errorf("%s operation=PlanJSONAgain observed_error=%v expected=nil", label, err)
	}
	secondPlanJSON, err := checkFuzzPlanJSON(label, "MarshalPlanJSONAgain", secondPlan)
	if err != nil {
		return err
	}
	if !bytes.Equal(firstPlanJSON, secondPlanJSON) {
		return fmt.Errorf("%s operation=PlanJSONDeterminism observed=%s expected=%s", label, secondPlanJSON, firstPlanJSON)
	}
	canonicalPlan, err := PlanJSON(canonicalOld, canonicalCurrent, contractDiffFuzzPlanOptions())
	if err != nil {
		return fmt.Errorf("%s operation=PlanCanonicalJSON observed_error=%v expected=nil", label, err)
	}
	canonicalPlanJSON, err := checkFuzzPlanJSON(label, "MarshalCanonicalPlanJSON", canonicalPlan)
	if err != nil {
		return err
	}
	if !bytes.Equal(firstPlanJSON, canonicalPlanJSON) {
		return fmt.Errorf("%s operation=PlanCanonicalEquivalence observed=%s expected=%s", label, canonicalPlanJSON, firstPlanJSON)
	}
	return nil
}

func checkFuzzPolicyResult(label, operation string, result PolicyResult, opts PolicyOptions) error {
	if got, want := result.Failed, opts.Strict && len(result.Warnings) > 0; got != want {
		return fmt.Errorf("%s operation=%s observed_failed=%v expected_failed=%v warnings=%q",
			label, operation, got, want, policyWarningSignatures(result.Warnings))
	}
	return nil
}

func checkFuzzPolicyEquivalence(label, operation string, got, want PolicyResult) error {
	if got.Failed != want.Failed {
		return fmt.Errorf("%s operation=%s observed_failed=%v expected_failed=%v observed_warnings=%q expected_warnings=%q",
			label, operation, got.Failed, want.Failed, policyWarningSignatures(got.Warnings), policyWarningSignatures(want.Warnings))
	}
	gotWarnings := policyWarningSignatures(got.Warnings)
	wantWarnings := policyWarningSignatures(want.Warnings)
	if !equalStringSlices(gotWarnings, wantWarnings) {
		return fmt.Errorf("%s operation=%s observed_warnings=%q expected_warnings=%q",
			label, operation, gotWarnings, wantWarnings)
	}
	return nil
}

func checkPlanJSONInvalidContractError(label string, oldData, currentData []byte) error {
	_, err := PlanJSON(oldData, currentData, contractDiffFuzzPlanOptions())
	if err == nil {
		return fmt.Errorf("%s operation=PlanJSONAfterCompareReject observed_error=nil expected_wrapped=%v", label, ErrInvalidContractJSON)
	}
	if !errors.Is(err, ErrInvalidContractJSON) {
		return fmt.Errorf("%s operation=PlanJSONAfterCompareReject observed_error=%v expected_wrapped=%v", label, err, ErrInvalidContractJSON)
	}
	return nil
}

func checkFuzzPlanJSON(label, operation string, plan MigrationPlan) ([]byte, error) {
	data, err := plan.MarshalCanonicalJSON()
	if err != nil {
		return nil, fmt.Errorf("%s operation=%s observed_error=%v expected=nil", label, operation, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("%s operation=%s observed_trailing_newline=%v expected=true plan=%s",
			label, operation, len(data) > 0 && data[len(data)-1] == '\n', data)
	}
	return data, nil
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
