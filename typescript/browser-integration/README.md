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

The strict-auth test verifies that browser-native `WebSocket` clients can observe
Shunter auth rejection close frames and that `@shunter/client` classifies the
same live rejection as `ShunterAuthError`.
