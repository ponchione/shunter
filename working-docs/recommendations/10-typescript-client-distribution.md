# Settle TypeScript Client Distribution

Status: recommended productization decision

Promotion trigger: an application outside the Shunter repository needs a
repeatable supported install and upgrade path.

Owners: TypeScript client, codegen, release process, documentation

## Why

The package-shaped `@shunter/client` runtime, generated bindings, local pack
smoke tests, and browser lifecycle already work. The unresolved question is
whether this becomes a governed public npm package or remains an intentionally
private/vendored dependency.

## Decision Inputs

- ownership of the `@shunter` npm scope
- release authority and required review
- npm access and 2FA policy
- package license and metadata
- version synchronization with root `VERSION`
- provenance/signing expectations
- checked-in versus release-built `dist` artifacts
- support expectations for browser, Electron, SSR, and injected WebSocket hosts
- upgrade and compatibility policy for generated bindings

## Work

1. Choose public publication or document private/vendored distribution as the
   supported product choice.
2. Automate the chosen path without weakening existing pack/install smoke
   coverage.
3. Make generated runtime import configuration and package version checks part
   of the release process.
4. Add framework helpers only after repeated real-app lifecycle code shows a
   stable abstraction.
5. Document rollback and compatibility behavior when app bindings and runtime
   versions diverge.

## Non-Goals

- multi-language SDK parity
- broad Node, Deno, Bun, Workers, React, Vue, and Svelte support in one slice
- publishing before ownership and security controls exist
- changing protocol behavior to resemble another SDK

## Completion Evidence

- one supported install path usable from an external repository
- package version and generated metadata match the Shunter release
- clean install, packed install, upgrade, and stale-binding failures are tested
- release and rollback authority is documented
- public documentation does not imply unsupported host/framework coverage
