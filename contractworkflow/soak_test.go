package contractworkflow

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/codegen"
	"github.com/ponchione/shunter/contractdiff"
	"github.com/ponchione/shunter/schema"
)

func TestReadOnlyWorkflowConcurrentShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xc0a7f10e)
		workers    = 6
		iterations = 128
	)

	fixture := newContractWorkflowSoakFixture(t)
	start := make(chan struct{})
	failures := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				if err := checkContractWorkflowSoakFixture(fixture); err != nil {
					select {
					case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d failure=%v",
						seed, worker, op, workers, iterations, err):
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

type contractWorkflowSoakFixture struct {
	previousPath string
	currentPath  string
	policyOpts   contractdiff.PolicyOptions
	planOpts     contractdiff.PlanOptions
	wantDiff     workflowFormatOutputs
	wantPolicy   workflowFormatOutputs
	wantPlan     workflowFormatOutputs
	wantCodegen  []byte
}

type workflowFormatOutputs struct {
	text []byte
	json []byte
}

func newContractWorkflowSoakFixture(t *testing.T) contractWorkflowSoakFixture {
	t.Helper()

	dir := t.TempDir()
	previousPath := writeContractFixture(t, dir, "previous.json", workflowContractFixture())
	current := workflowContractFixture()
	current.Module.Version = "v1.1.0"
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	current.Schema.Reducers = nil
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{{Name: "history", Required: []string{"messages:read"}}}
	currentPath := writeContractFixture(t, dir, "current.json", current)

	policyOpts := contractdiff.PolicyOptions{
		RequirePreviousVersion: true,
		Strict:                 true,
	}
	planOpts := contractdiff.PlanOptions{
		Policy: contractdiff.PolicyOptions{RequirePreviousVersion: true},
	}
	wantCodegen, err := codegen.Generate(current, codegen.Options{Language: codegen.LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	fixture := contractWorkflowSoakFixture{
		previousPath: previousPath,
		currentPath:  currentPath,
		policyOpts:   policyOpts,
		planOpts:     planOpts,
		wantCodegen:  wantCodegen,
	}

	report, err := CompareFiles(previousPath, currentPath)
	if err != nil {
		t.Fatalf("CompareFiles returned error: %v", err)
	}
	fixture.wantDiff = mustWorkflowFormatOutputs(t, "FormatDiff", func(format string) ([]byte, error) {
		return FormatDiff(report, format)
	})

	policy, err := CheckPolicyFiles(previousPath, currentPath, policyOpts)
	if err != nil {
		t.Fatalf("CheckPolicyFiles returned error: %v", err)
	}
	fixture.wantPolicy = mustWorkflowFormatOutputs(t, "FormatPolicy", func(format string) ([]byte, error) {
		return FormatPolicy(policy, format)
	})

	plan, err := PlanFiles(previousPath, currentPath, planOpts)
	if err != nil {
		t.Fatalf("PlanFiles returned error: %v", err)
	}
	fixture.wantPlan = mustWorkflowFormatOutputs(t, "FormatPlan", func(format string) ([]byte, error) {
		return FormatPlan(plan, format)
	})

	return fixture
}

func checkContractWorkflowSoakFixture(fixture contractWorkflowSoakFixture) error {
	report, err := CompareFiles(fixture.previousPath, fixture.currentPath)
	if err != nil {
		return fmt.Errorf("operation=CompareFiles observed_error=%v expected=nil", err)
	}
	if err := checkWorkflowFormatOutputs("FormatDiff", fixture.wantDiff, func(format string) ([]byte, error) {
		return FormatDiff(report, format)
	}); err != nil {
		return err
	}

	policy, err := CheckPolicyFiles(fixture.previousPath, fixture.currentPath, fixture.policyOpts)
	if err != nil {
		return fmt.Errorf("operation=CheckPolicyFiles observed_error=%v expected=nil", err)
	}
	if err := checkWorkflowFormatOutputs("FormatPolicy", fixture.wantPolicy, func(format string) ([]byte, error) {
		return FormatPolicy(policy, format)
	}); err != nil {
		return err
	}

	plan, err := PlanFiles(fixture.previousPath, fixture.currentPath, fixture.planOpts)
	if err != nil {
		return fmt.Errorf("operation=PlanFiles observed_error=%v expected=nil", err)
	}
	if err := checkWorkflowFormatOutputs("FormatPlan", fixture.wantPlan, func(format string) ([]byte, error) {
		return FormatPlan(plan, format)
	}); err != nil {
		return err
	}

	generated, err := GenerateFromFile(fixture.currentPath, codegen.Options{Language: codegen.LanguageTypeScript})
	if err != nil {
		return fmt.Errorf("operation=GenerateFromFile observed_error=%v expected=nil", err)
	}
	if !bytes.Equal(generated, fixture.wantCodegen) {
		return fmt.Errorf("operation=GenerateFromFileDeterminism observed_len=%d expected_len=%d", len(generated), len(fixture.wantCodegen))
	}

	return nil
}

func mustWorkflowFormatOutputs(t *testing.T, operation string, format func(string) ([]byte, error)) workflowFormatOutputs {
	t.Helper()
	outputs, err := workflowFormatOutputsFor(operation, format)
	if err != nil {
		t.Fatal(err)
	}
	return outputs
}

func checkWorkflowFormatOutputs(operation string, want workflowFormatOutputs, format func(string) ([]byte, error)) error {
	got, err := workflowFormatOutputsFor(operation, format)
	if err != nil {
		return err
	}
	if !bytes.Equal(got.text, want.text) {
		return fmt.Errorf("operation=%s format=%s observed=%q expected=%q", operation, FormatText, got.text, want.text)
	}
	if !bytes.Equal(got.json, want.json) {
		return fmt.Errorf("operation=%s format=%s observed=%q expected=%q", operation, FormatJSON, got.json, want.json)
	}
	return nil
}

func workflowFormatOutputsFor(operation string, format func(string) ([]byte, error)) (workflowFormatOutputs, error) {
	text, err := format(FormatText)
	if err != nil {
		return workflowFormatOutputs{}, fmt.Errorf("operation=%s format=%s observed_error=%v expected=nil", operation, FormatText, err)
	}
	json, err := format(FormatJSON)
	if err != nil {
		return workflowFormatOutputs{}, fmt.Errorf("operation=%s format=%s observed_error=%v expected=nil", operation, FormatJSON, err)
	}
	return workflowFormatOutputs{text: text, json: json}, nil
}
