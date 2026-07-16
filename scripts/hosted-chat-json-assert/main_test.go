package main

import (
	"strings"
	"testing"
)

const validQueryReport = `{
  "status":"ok",
  "scope":"running_app",
  "command":"query",
  "module":"hosted_chat",
  "surface":"recent_messages",
  "result":{"name":"recent_messages","table_name":"messages","rows":[
    {"id":"2","author":"System","body":"procedure body"},
    {"id":"1","author":"Ada","body":"reducer body"}
  ]},
  "diagnostic":"ignored"
}`

func TestAssertQueryRequiresBodiesInExactResultRows(t *testing.T) {
	want := []messageRow{
		{ID: "2", Author: "System", Body: "procedure body"},
		{ID: "1", Author: "Ada", Body: "reducer body"},
	}
	if err := assertQuery([]byte(validQueryReport), "recent_messages", "recent_messages", "messages", want); err != nil {
		t.Fatalf("assertQuery valid report: %v", err)
	}

	diagnosticOnly := strings.Replace(validQueryReport, `"body":"reducer body"`, `"body":"wrong body"`, 1)
	diagnosticOnly = strings.Replace(diagnosticOnly, `"diagnostic":"ignored"`, `"diagnostic":"reducer body"`, 1)
	if err := assertQuery([]byte(diagnosticOnly), "recent_messages", "recent_messages", "messages", want); err == nil {
		t.Fatal("assertQuery accepted a body present only outside result.rows")
	}
}

func TestAssertQueryRejectsDuplicateRows(t *testing.T) {
	duplicate := strings.Replace(validQueryReport,
		`{"id":"1","author":"Ada","body":"reducer body"}`,
		`{"id":"2","author":"System","body":"procedure body"}`,
		1,
	)
	want := []messageRow{
		{ID: "2", Author: "System", Body: "procedure body"},
		{ID: "1", Author: "Ada", Body: "reducer body"},
	}
	if err := assertQuery([]byte(duplicate), "recent_messages", "recent_messages", "messages", want); err == nil {
		t.Fatal("assertQuery accepted an unexpected duplicate row")
	}
}

func TestAssertQueryEquivalentIgnoresVolatileFieldsButNotRows(t *testing.T) {
	after := strings.Replace(validQueryReport, `"diagnostic":"ignored"`, `"target_url":"ws://restored"`, 1)
	if err := assertQueryEquivalent([]byte(validQueryReport), []byte(after)); err != nil {
		t.Fatalf("assertQueryEquivalent volatile fields: %v", err)
	}
	changed := strings.Replace(after, `"body":"procedure body"`, `"body":"changed"`, 1)
	if err := assertQueryEquivalent([]byte(validQueryReport), []byte(changed)); err == nil {
		t.Fatal("assertQueryEquivalent accepted changed restored rows")
	}
}
