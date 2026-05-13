# Use Generated TypeScript Clients

Status: current v1 app-author guidance
Scope: installing the local TypeScript SDK runtime, generating module bindings,
and using the generated helpers from browser or Electron clients.

Shunter's TypeScript path has two pieces:

- the private local SDK runtime package in `typescript/client`, published to
  apps as `@shunter/client`
- generated module bindings from `shunter contract codegen --language
  typescript`

The generated file imports shared runtime types and helpers from
`@shunter/client` by default. Keep those two pieces versioned together with the
contract they were generated from.

## Install The Runtime Package

For v1, use a local package path, workspace package, or packed tarball. Public
npm publishing is not part of the v1 contract.

App `package.json` with a `file:` dependency:

```json
{
  "dependencies": {
    "@shunter/client": "file:../shunter/typescript/client"
  }
}
```

Packed tarball dependency:

```json
{
  "dependencies": {
    "@shunter/client": "file:./vendor/shunter-client-1.0.0.tgz"
  }
}
```

Workspace installs should still resolve the package name as `@shunter/client`
unless the app intentionally vendors it under its own package name.

Build the runtime package before consuming it through a local path or tarball:

```bash
rtk npm --prefix typescript/client run build
rtk npm --prefix typescript/client run pack:dry-run
```

## Generate Bindings

Generate bindings from a reviewed contract artifact:

```bash
rtk go run ./cmd/shunter contract codegen --contract shunter.contract.json --language typescript --out src/shunter.gen.ts
```

If the app renames or vendors the runtime package, generate with the same import
specifier:

```bash
rtk go run ./cmd/shunter contract codegen --contract shunter.contract.json --language typescript --runtime-import @app/shunter-runtime --out src/shunter.gen.ts
```

The Go API equivalent is:

```go
codegen.Options{
	Language:                codegen.LanguageTypeScript,
	TypeScriptRuntimeImport: "@app/shunter-runtime",
}
```

Generated identifier normalization is stable for v1 output. Names are emitted
as TypeScript-safe identifiers by splitting on non-letter and non-digit
separators, applying the category's camel-case or Pascal-case style, prefixing
leading digits with `_`, suffixing reserved words with `_`, and appending
numeric collision suffixes in contract order.

Generated TypeScript is intended for browsers and Electron renderers with
standard Web APIs. Non-browser hosts must provide a compatible
`webSocketFactory`. Server-side SDK APIs, framework cache adapters, generated
writes that bypass reducers, and SpacetimeDB client API compatibility are out
of scope for v1.

## Connect

Pass the generated protocol and contract metadata into the runtime client. When
`contract` is supplied, `createShunterClient` checks the generated contract
format/version and protocol metadata before opening the WebSocket.

```ts
import {
  assertGeneratedContractCompatible,
  createShunterClient,
} from "@shunter/client";
import {
  shunterContract,
  shunterProtocol,
} from "./shunter.gen";

assertGeneratedContractCompatible(shunterContract, {
  moduleName: "chat",
  moduleVersion: "v0.1.0",
});

const client = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  contract: shunterContract,
  token: () => authToken,
});

await client.connect();
```

The default browser path uses global `WebSocket`.

Generated protocol metadata defaults to `v2.bsatn.shunter` while still listing
`v1.bsatn.shunter` as supported. Parameterized declared reads require a
negotiated v2 connection; no-parameter declared reads remain compatible with
v1.

## Call Reducers

Generated bindings always keep raw `Uint8Array` reducer helpers. When a
contract exports reducer argument product schemas, generated bindings also
include schema-aware argument encoders and typed helper wrappers.

```ts
import {
  callSendMessage,
  callSendMessageResult,
  callSendMessageTyped,
} from "./shunter.gen";

await callSendMessage(client.callReducer, rawArgs);

await callSendMessageTyped(client.callReducer, {
  channel: "general",
  body: "hello",
});

const result = await callSendMessageResult(client.callReducer, rawArgs);
if (result.status === "failed") {
  throw result.error;
}
```

If a reducer does not export product schemas, keep the app's byte encoding
documented near the reducer and pass encoded `Uint8Array` values through the
raw helper.

## Read Queries

Executable declared queries get raw helpers. When exported declared-read row
metadata is available, generated bindings also expose decoded result helpers
that use generated row decoders by default. Parameterized declared queries get
typed params interfaces and helpers that encode those params before calling the
runtime.

```ts
import {
  queryMessagesByTopicDecoded,
  queryRecentMessages,
  queryRecentMessagesDecoded,
} from "./shunter.gen";

const rawFrame = await queryRecentMessages(client.runDeclaredQuery);
void rawFrame;

const decoded = await queryRecentMessagesDecoded(client.runDeclaredQuery);
for (const row of decoded.tables[0]?.rows ?? []) {
  console.log(row.body);
}

const byTopic = await queryMessagesByTopicDecoded(
  client.runDeclaredQuery,
  { topic: "general", afterId: 1n },
);
for (const row of byTopic.tables[0]?.rows ?? []) {
  console.log(row.body);
}
```

Use raw helpers when the app wants bytes or custom decoding. Use decoded helpers
when the generated contract row schema is the client-facing shape. Generated
parameterized helpers hide BSATN encoding; the lower-level runtime
`DeclaredQueryOptions.params` field is an already encoded `Uint8Array`.
Supplying `params` on a v1 connection raises a protocol mismatch before sending.
No-parameter helper signatures are unchanged.

## Subscribe To Tables And Views

Generated table helpers install generated table row decoders by default, so
their callbacks receive typed rows. Raw table subscribers without a decoder
receive cloned row bytes.

```ts
import {
  subscribeLiveMessagesByTopic,
  subscribeLiveMessages,
  subscribeLiveMessagesHandle,
  subscribeMessages,
} from "./shunter.gen";

const unsubscribeMessages = await subscribeMessages(
  client.subscribeTable,
  (rows) => {
    console.log(rows.length);
  },
);

const liveHandle = await subscribeLiveMessagesHandle(
  client.subscribeDeclaredView,
  { returnHandle: true },
);

const unsubscribeByTopic = await subscribeLiveMessagesByTopic(
  client.subscribeDeclaredView,
  { topic: "general" },
  {
    onUpdate: (update) => {
      console.log(update.inserts.length);
    },
  },
);

await unsubscribeMessages();
await unsubscribeByTopic();
await liveHandle.unsubscribe();
```

Managed handles track `subscribing`, `active`, `unsubscribing`, and `closed`
states. Unsubscribe paths wait for the matching server acknowledgement or a
matching subscription error.

## Reconnect

Reconnect is explicit opt-in:

```ts
const client = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  contract: shunterContract,
  token: () => refreshToken(),
  reconnect: {
    enabled: true,
    maxAttempts: 3,
    initialDelayMs: 250,
    maxDelayMs: 5000,
  },
});
```

When reconnect is enabled, token providers are called for each connection
attempt. Resubscription is enabled by default after a fresh identity handshake.
A disconnected interval is still a cache boundary: re-read or use the replayed
initial snapshot after reconnect when the client needs an authoritative view.

## Verify SDK Changes

Run the SDK checks from the Shunter checkout when touching the runtime package
or generated TypeScript surface:

```bash
rtk npm --prefix typescript/client run test
rtk npm --prefix typescript/client run build
rtk npm --prefix typescript/client run smoke:package
```
