# Testing Shunter Modules

Status: current v1 app-author guidance
Scope: testing app modules that embed Shunter.

Most Shunter app behavior can be tested through the root runtime API. Prefer
tests that build the real module, use a temporary `DataDir`, call reducers, and
read state through public runtime surfaces.

## Basic Runtime Test

```go
func TestSendMessage(t *testing.T) {
	ctx := context.Background()

	rt, err := shunter.Build(app.Module(), shunter.Config{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})

	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}

	res, err := rt.CallReducer(ctx, "send_message", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != shunter.StatusCommitted {
		t.Fatalf("send_message status = %v, error = %v", res.Status, res.Error)
	}
}
```

Use `t.TempDir()` unless the test is intentionally verifying recovery from a
specific durable directory.

## Verify State

Use `Runtime.Read` for local state assertions.

```go
err := rt.Read(ctx, func(view shunter.LocalReadView) error {
	if got := view.RowCount(messagesTableID); got != 1 {
		return fmt.Errorf("messages row count = %d, want 1", got)
	}
	return nil
})
if err != nil {
	t.Fatal(err)
}
```

Use declared reads when the contract surface matters:

```go
result, err := rt.CallQuery(ctx, "recent_messages")
if err != nil {
	t.Fatal(err)
}
if len(result.Rows) != 1 {
	t.Fatalf("rows = %d, want 1", len(result.Rows))
}
```

## Test Permissions

Attach permission metadata to reducers, queries, views, and tables. Then test
allowed and denied callers through the same runtime path the app uses.

In dev mode, local calls without explicit permissions allow all permissions.
To test denial, pass an explicit permission set that does not satisfy the
declared requirement.

```go
_, err := rt.CallQuery(
	ctx,
	"recent_messages",
	shunter.WithDeclaredReadPermissions("wrong:permission"),
)
if err == nil {
	t.Fatal("CallQuery succeeded without required permission")
}
```

For strict auth behavior over the protocol path, configure `AuthModeStrict` and
use real signed tokens in integration tests. See
[Authentication](../authentication.md).

## Test Contract Output

Export contracts in tests when app-facing declarations are important release
artifacts.

```go
contractJSON, err := rt.ExportContractJSON()
if err != nil {
	t.Fatal(err)
}
if err := shunter.ValidateModuleContract(rt.ExportContract()); err != nil {
	t.Fatal(err)
}
_ = contractJSON
```

Golden contract tests are useful when a client or separate example repository
depends on stable names, columns, permissions, or read surfaces.

## Test Recovery

Recovery tests need a persistent directory across runtime instances.

```go
dataDir := t.TempDir()

rt1, err := shunter.Build(app.Module(), shunter.Config{DataDir: dataDir})
if err != nil {
	t.Fatal(err)
}
// Start, write data, wait for durability, close.
_ = rt1

rt2, err := shunter.Build(app.Module(), shunter.Config{DataDir: dataDir})
if err != nil {
	t.Fatal(err)
}
t.Cleanup(func() { _ = rt2.Close() })
```

Use `WaitUntilDurable` when the assertion depends on a particular transaction
being durable before closing or rebuilding.

## Test Protocol Serving

Use protocol tests when the client-facing WebSocket path matters. Start a
runtime with `EnableProtocol: true`, serve `Runtime.HTTPHandler()` through an
`httptest.Server`, and connect with the protocol client path used by the app or
SDK.

Prefer local runtime tests for reducer and declared-read business logic, then
add protocol tests for auth, WebSocket admission, message encoding, and live
delivery behavior.

## Test Checklist

- Build the real module.
- Use `t.TempDir()` for `DataDir`.
- Start before calling reducers or reads.
- Always close the runtime.
- Assert reducer status, not just Go error.
- Verify state through public read APIs.
- Test denied permissions as well as allowed permissions.
- Export or validate contracts for release-facing module changes.
- Add recovery tests for persistence-sensitive behavior.
