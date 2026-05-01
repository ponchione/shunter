package shunter

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestRootImportGraphExcludesPrometheus(t *testing.T) {
	cmd := exec.Command("go", "list", "-json", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -json .: %v\n%s", err, out)
	}

	var pkg struct {
		Imports []string
		Deps    []string
	}
	if err := json.Unmarshal(out, &pkg); err != nil {
		t.Fatalf("decode go list output: %v\n%s", err, out)
	}

	for _, path := range append(pkg.Imports, pkg.Deps...) {
		if strings.HasPrefix(path, "github.com/prometheus/") {
			t.Fatalf("root package import graph includes Prometheus package %q", path)
		}
	}
}
