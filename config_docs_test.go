package shunter

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/subscription"
)

func TestConfigReferencePinsZeroValueWorkAndMultiJoinDefaults(t *testing.T) {
	data, err := os.ReadFile("docs/reference/config.md")
	if err != nil {
		t.Fatalf("read config reference: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	tests := []struct {
		field string
		value int
	}{
		{field: "OneOffQueryMaxWork", value: protocol.DefaultSQLQueryMaxWork},
		{field: "SubscriptionOrderedWindowMaxRows", value: subscription.DefaultOrderedWindowMaxRows},
		{field: "SubscriptionMaxMultiJoinRelations", value: subscription.DefaultMultiJoinMaxRelations},
		{field: "SubscriptionMaxMultiJoinRowsPerRelation", value: subscription.DefaultMultiJoinMaxRowsPerRelation},
		{field: "SubscriptionMaxMultiJoinWork", value: subscription.DefaultMultiJoinMaxWork},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			var fieldLine string
			for _, line := range lines {
				if strings.Contains(line, "| `"+tt.field+"`") {
					fieldLine = line
					break
				}
			}
			if fieldLine == "" {
				t.Fatalf("config reference does not document %s", tt.field)
			}
			want := "Zero uses " + formatDocumentationInteger(tt.value)
			if !strings.Contains(fieldLine, want) {
				t.Fatalf("config reference line = %q, want %q", fieldLine, want)
			}
		})
	}
}

func formatDocumentationInteger(value int) string {
	raw := strconv.Itoa(value)
	for i := len(raw) - 3; i > 0; i -= 3 {
		raw = raw[:i] + "," + raw[i:]
	}
	return raw
}
