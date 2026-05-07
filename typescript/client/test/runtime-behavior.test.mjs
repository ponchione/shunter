import assert from "node:assert/strict";
import {
  SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY,
  SHUNTER_CLIENT_MESSAGE_CALL_REDUCER,
  SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE,
  SHUNTER_SUBPROTOCOL_V1,
  ShunterAuthError,
  ShunterClosedClientError,
  ShunterProtocolError,
  ShunterProtocolMismatchError,
  ShunterValidationError,
  assertProtocolCompatible,
  checkProtocolCompatibility,
  createShunterClient,
  createSubscriptionHandle,
  decodeIdentityTokenFrame,
  decodeTransactionUpdateFrame,
  encodeReducerCallRequest,
  selectShunterSubprotocol,
  shunterProtocol,
} from "../.tmp_runtime_test/src/index.js";

assert.equal(selectShunterSubprotocol(shunterProtocol), SHUNTER_SUBPROTOCOL_V1);
assert.equal(assertProtocolCompatible(shunterProtocol), SHUNTER_SUBPROTOCOL_V1);
assert.deepEqual(checkProtocolCompatibility(shunterProtocol), {
  ok: true,
  subprotocol: SHUNTER_SUBPROTOCOL_V1,
});

assert.throws(
  () =>
    assertProtocolCompatible({
      minSupportedVersion: 2,
      currentVersion: 2,
      defaultSubprotocol: "v2.bsatn.shunter",
      supportedSubprotocols: ["v2.bsatn.shunter"],
    }),
  ShunterProtocolMismatchError,
);

assert.throws(
  () => assertProtocolCompatible(shunterProtocol, "v1.bsatn.spacetimedb"),
  ShunterProtocolMismatchError,
);

const states = [];
let unsubscribeCalls = 0;
const handle = createSubscriptionHandle({
  queryId: 7,
  initialRows: [{ id: 1 }],
  onStateChange: (state) => states.push(state.status),
  unsubscribe: async () => {
    unsubscribeCalls += 1;
  },
});

assert.equal(handle.queryId, 7);
assert.deepEqual(handle.state, { status: "active", rows: [{ id: 1 }] });

handle.replaceRows([{ id: 2 }]);
assert.deepEqual(handle.state, { status: "active", rows: [{ id: 2 }] });

await handle.unsubscribe();
await handle.unsubscribe();
assert.equal(unsubscribeCalls, 1);
assert.deepEqual(await handle.closed, { reason: "unsubscribed" });
assert.deepEqual(handle.state, { status: "closed" });
assert.deepEqual(states, ["active", "unsubscribing", "closed"]);

assert.throws(
  () => handle.replaceRows([{ id: 3 }]),
  ShunterClosedClientError,
);

const failing = createSubscriptionHandle({
  unsubscribe: () => {
    throw new Error("denied");
  },
});
await failing.unsubscribe();
const failedClosed = await failing.closed;
assert.equal(failedClosed.reason, "error");
assert.equal(failedClosed.error.kind, "transport");
assert.match(failedClosed.error.message, /denied/);

class FakeWebSocket {
  constructor(url, protocols) {
    this.url = url;
    this.protocols = protocols;
    this.protocol = protocols[0] ?? "";
    this.binaryType = "blob";
    this.closeCalls = [];
    this.sent = [];
    this.listeners = new Map();
  }

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type, listener) {
    this.listeners.get(type)?.delete(listener);
  }

  close(code = 1000, reason = "") {
    this.closeCalls.push({ code, reason });
    this.dispatch("close", { code, reason, wasClean: true });
  }

  send(data) {
    this.sent.push(data);
  }

  open(protocol = this.protocol) {
    this.protocol = protocol;
    this.dispatch("open", {});
  }

  message(data) {
    this.dispatch("message", { data });
  }

  error() {
    this.dispatch("error", {});
  }

  dispatch(type, event) {
    for (const listener of [...(this.listeners.get(type) ?? [])]) {
      listener(event);
    }
  }
}

const sockets = [];
const fakeFactory = (url, protocols) => {
  const socket = new FakeWebSocket(url, protocols);
  sockets.push(socket);
  return socket;
};

function identityTokenFrame({ identityStart = 1, token = "server-token", connectionStart = 0xa0 } = {}) {
  const tokenBytes = new TextEncoder().encode(token);
  const frame = new Uint8Array(1 + 32 + 4 + tokenBytes.length + 16);
  let offset = 0;
  frame[offset] = 1;
  offset += 1;
  for (let i = 0; i < 32; i += 1) {
    frame[offset + i] = identityStart + i;
  }
  offset += 32;
  new DataView(frame.buffer).setUint32(offset, tokenBytes.length, true);
  offset += 4;
  frame.set(tokenBytes, offset);
  offset += tokenBytes.length;
  for (let i = 0; i < 16; i += 1) {
    frame[offset + i] = connectionStart + i;
  }
  return frame;
}

const decodedIdentity = decodeIdentityTokenFrame(identityTokenFrame());
assert.equal(decodedIdentity.token, "server-token");
assert.deepEqual([...decodedIdentity.identity.slice(0, 3)], [1, 2, 3]);
assert.deepEqual([...decodedIdentity.connectionId.slice(0, 3)], [0xa0, 0xa1, 0xa2]);
assert.throws(
  () => decodeIdentityTokenFrame(new Uint8Array([2])),
  ShunterProtocolError,
);

function bytesFromHex(hex) {
  return Uint8Array.from(hex.match(/../g).map((byte) => Number.parseInt(byte, 16)));
}

const encodedReducer = encodeReducerCallRequest("send", new Uint8Array([0xaa, 0xbb]), {
  requestId: 0x31323334,
  noSuccessNotify: true,
});
assert.equal(encodedReducer.name, "send");
assert.equal(encodedReducer.requestId, 0x31323334);
assert.equal(encodedReducer.flags, SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY);
assert.equal(encodedReducer.frame[0], SHUNTER_CLIENT_MESSAGE_CALL_REDUCER);
assert.deepEqual(
  encodedReducer.frame,
  bytesFromHex("030400000073656e6402000000aabb3433323101"),
);

assert.deepEqual(
  encodeReducerCallRequest("ping", new Uint8Array(), { requestId: 1 }).frame,
  bytesFromHex("030400000070696e67000000000100000000"),
);

assert.throws(
  () => encodeReducerCallRequest("send", new Uint8Array(), { requestId: 0x1_0000_0000 }),
  ShunterValidationError,
);

assert.throws(
  () => encodeReducerCallRequest("\ud800", new Uint8Array(), { requestId: 1 }),
  ShunterValidationError,
);

const committedUpdateFrame = bytesFromHex(
  "050001000000040302010500000075736572730f0000000200000002000000010201000000030200000004050807060504030201202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3fa0a1a2a3a4a5a6a7a8a9aaabacadaeaf0400000073656e641413121102000000aabb242322213837363534333231",
);
const committedUpdate = decodeTransactionUpdateFrame(committedUpdateFrame);
assert.equal(committedUpdateFrame[0], SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE);
assert.equal(committedUpdate.status.status, "committed");
assert.equal(committedUpdate.status.updates.length, 1);
assert.equal(committedUpdate.status.updates[0].queryId, 0x01020304);
assert.equal(committedUpdate.status.updates[0].tableName, "users");
assert.deepEqual([...committedUpdate.status.updates[0].inserts.slice(0, 4)], [0x02, 0x00, 0x00, 0x00]);
assert.equal(committedUpdate.timestamp, 0x0102030405060708n);
assert.deepEqual([...committedUpdate.callerIdentity.slice(0, 3)], [0x20, 0x21, 0x22]);
assert.deepEqual([...committedUpdate.callerConnectionId.slice(0, 3)], [0xa0, 0xa1, 0xa2]);
assert.equal(committedUpdate.reducerCall.name, "send");
assert.equal(committedUpdate.reducerCall.reducerId, 0x11121314);
assert.deepEqual(committedUpdate.reducerCall.args, new Uint8Array([0xaa, 0xbb]));
assert.equal(committedUpdate.reducerCall.requestId, 0x21222324);
assert.equal(committedUpdate.totalHostExecutionDuration, 0x3132333435363738n);
assert.deepEqual(committedUpdate.rawFrame, committedUpdateFrame);

const failedUpdateFrame = bytesFromHex(
  "050104000000626f6f6d18171615141312110000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000400000073656e640000000000000000242322210000000000000000",
);
const failedUpdate = decodeTransactionUpdateFrame(failedUpdateFrame);
assert.equal(failedUpdate.status.status, "failed");
assert.equal(failedUpdate.status.error, "boom");
assert.equal(failedUpdate.reducerCall.name, "send");
assert.equal(failedUpdate.reducerCall.requestId, 0x21222324);

const clientStates = [];
const client = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe?existing=1",
  protocol: shunterProtocol,
  token: "test-token",
  webSocketFactory: fakeFactory,
  onStateChange: ({ current }) => clientStates.push(current.status),
});
const connecting = client.connect();
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(sockets.length, 1);
assert.equal(sockets[0].url, "ws://127.0.0.1:3000/subscribe?existing=1&token=test-token");
assert.deepEqual(sockets[0].protocols, [SHUNTER_SUBPROTOCOL_V1]);
assert.equal(sockets[0].binaryType, "arraybuffer");
assert.deepEqual(clientStates, ["connecting"]);
sockets[0].open();
assert.equal(client.state.status, "connecting");
sockets[0].message(identityTokenFrame({ token: "minted-token" }).buffer);
const metadata = await connecting;
assert.equal(metadata.subprotocol, SHUNTER_SUBPROTOCOL_V1);
assert.equal(metadata.identityToken, "minted-token");
assert.deepEqual([...metadata.identity.slice(0, 3)], [1, 2, 3]);
assert.deepEqual([...metadata.connectionId.slice(0, 3)], [0xa0, 0xa1, 0xa2]);
assert.equal(client.state.status, "connected");
const reducerResponse = client.callReducer("send", new Uint8Array([0xaa, 0xbb]), {
  requestId: 0x21222324,
});
assert.equal(sockets[0].sent.length, 1);
sockets[0].message(committedUpdateFrame);
assert.deepEqual(await reducerResponse, committedUpdateFrame);

const reducerFailure = client.callReducer("send", new Uint8Array(), {
  requestId: 0x21222324,
});
assert.equal(sockets[0].sent.length, 2);
sockets[0].message(failedUpdateFrame);
await assert.rejects(reducerFailure, ShunterValidationError);

const sentReducer = await client.callReducer("send", new Uint8Array([0xaa, 0xbb]), {
  requestId: 0x31323334,
  noSuccessNotify: true,
});
assert.deepEqual(sentReducer, encodedReducer.frame);
assert.equal(sockets[0].sent.length, 3);
assert.deepEqual(sockets[0].sent[2], encodedReducer.frame);
await client.close();
await client.close();
await client.dispose();
assert.equal(sockets[0].closeCalls.length, 1);
assert.deepEqual(clientStates, ["connecting", "connected", "closing", "closed"]);
await assert.rejects(
  client.callReducer("send", new Uint8Array(), { requestId: 1 }),
  ShunterClosedClientError,
);

const tokenFailureClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    throw new Error("no token");
  },
  webSocketFactory: fakeFactory,
});
await assert.rejects(tokenFailureClient.connect(), ShunterAuthError);
assert.equal(tokenFailureClient.state.status, "failed");

const mismatchClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: fakeFactory,
});
const mismatchConnecting = mismatchClient.connect();
await new Promise((resolve) => setTimeout(resolve, 0));
const mismatchSocket = sockets.at(-1);
mismatchSocket.open("v1.bsatn.spacetimedb");
await assert.rejects(mismatchConnecting, ShunterProtocolMismatchError);
assert.equal(mismatchClient.state.status, "failed");
assert.equal(mismatchSocket.closeCalls.length, 1);

const malformedClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: fakeFactory,
});
const malformedConnecting = malformedClient.connect();
await new Promise((resolve) => setTimeout(resolve, 0));
const malformedSocket = sockets.at(-1);
malformedSocket.open();
malformedSocket.message(new Uint8Array([1, 2, 3]));
await assert.rejects(malformedConnecting, ShunterProtocolError);
assert.equal(malformedClient.state.status, "failed");
