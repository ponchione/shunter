# Shunter Browser Integration

This package owns browser-only protocol checks that should not live in Go unit
tests. It uses Node Playwright against a live Shunter strict-auth fixture server.

Setup:

```sh
rtk npm ci --prefix typescript/browser-integration
rtk npm --prefix typescript/browser-integration run install:browsers
```

Run:

```sh
rtk npm --prefix typescript/browser-integration test
```

The strict-auth scenario verifies that browser-native `WebSocket` clients can
observe Shunter auth rejection close frames and that `@shunter/client`
classifies the same live rejection as `ShunterAuthError`.

The successful-lifecycle scenario connects to a real runtime, subscribes to a
public table, observes a durable server-side update, kills and restarts the
fixture to force an abnormal native transport loss, and verifies replay at a
new synchronization epoch. It then awaits unsubscribe and proves that a
captured late frame dispatched on the old native socket cannot change the
closed handle or invoke cache callbacks.
