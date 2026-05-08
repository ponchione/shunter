import assert from "node:assert/strict";
import {
  SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY,
  SHUNTER_CLIENT_MESSAGE_CALL_REDUCER,
  SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY,
  SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_SINGLE,
  SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW,
  SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_SINGLE,
  SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_MULTI,
  SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE,
  SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED,
  SHUNTER_SERVER_MESSAGE_SUBSCRIBE_MULTI_APPLIED,
  SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR,
  SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE,
  SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT,
  SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED,
  SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED,
  SHUNTER_SUBPROTOCOL_V1,
  ShunterAuthError,
  ShunterClosedClientError,
  ShunterProtocolError,
  ShunterProtocolMismatchError,
  ShunterTransportError,
  ShunterValidationError,
  assertProtocolCompatible,
  callReducerWithEncodedArgs,
  callReducerWithEncodedArgsResult,
  callReducerWithResult,
  checkProtocolCompatibility,
  createShunterClient,
  createSubscriptionHandle,
  decodeBsatnProduct,
  decodeDeclaredQueryResult,
  decodeIdentityTokenFrame,
  decodeOneOffQueryResponseFrame,
  decodeRawDeclaredQueryResult,
  decodeReducerCallResult,
  decodeRowList,
  encodeReducerArgs,
  decodeSubscribeSingleAppliedFrame,
  decodeSubscribeMultiAppliedFrame,
  decodeSubscriptionErrorFrame,
  decodeTransactionUpdateLightFrame,
  decodeTransactionUpdateFrame,
  decodeUnsubscribeSingleAppliedFrame,
  decodeUnsubscribeMultiAppliedFrame,
  encodeBsatnProduct,
  encodeDeclaredQueryRequest,
  encodeDeclaredViewSubscriptionRequest,
  encodeReducerCallRequest,
  encodeSubscribeSingleRequest,
  encodeTableSubscriptionRequest,
  encodeUnsubscribeSingleRequest,
  encodeUnsubscribeMultiRequest,
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

const nextTurn = () => new Promise((resolve) => setTimeout(resolve, 0));

async function rejectByNextTurn(promise, validate) {
  let outcome = { status: "pending" };
  promise.then(
    (value) => {
      outcome = { status: "fulfilled", value };
    },
    (error) => {
      outcome = { status: "rejected", error };
    },
  );
  await nextTurn();
  if (outcome.status === "pending") {
    assert.fail("Expected promise to reject before the next turn.");
  }
  assert.equal(outcome.status, "rejected");
  validate?.(outcome.error);
  return outcome.error;
}

function assertSingleTokenUrl(rawUrl, expectedToken) {
  const parsed = new URL(rawUrl);
  assert.deepEqual(parsed.searchParams.getAll("token"), [expectedToken]);
  assert.equal(parsed.searchParams.get("existing"), "1");
}

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

const decodedBsatnMessage = decodeBsatnProduct(
  bytesFromHex("0801000000000000000b05000000616c6963650b000b0500000068656c6c6f110200000000000000"),
  [
    { name: "id", kind: "uint64" },
    { name: "sender", kind: "string" },
    { name: "topic", kind: "string", nullable: true },
    { name: "body", kind: "string" },
    { name: "sent_at", kind: "timestamp" },
  ],
  (values) => ({
    id: values[0],
    sender: values[1],
    topic: values[2],
    body: values[3],
    sentAt: values[4],
  }),
);
assert.deepEqual(decodedBsatnMessage, {
  id: 1n,
  sender: "alice",
  topic: null,
  body: "hello",
  sentAt: 2n,
});
const bsatnMessageColumns = [
  { name: "id", kind: "uint64" },
  { name: "sender", kind: "string" },
  { name: "topic", kind: "string", nullable: true },
  { name: "body", kind: "string" },
  { name: "sent_at", kind: "timestamp" },
];
const encodedBsatnMessage = encodeBsatnProduct(
  [1n, "alice", null, "hello", 2n],
  bsatnMessageColumns,
);
assert.deepEqual(
  encodedBsatnMessage,
  bytesFromHex("0801000000000000000b05000000616c6963650b000b0500000068656c6c6f110200000000000000"),
);
assert.deepEqual(
  decodeBsatnProduct(encodedBsatnMessage, bsatnMessageColumns, (values) => values),
  [1n, "alice", null, "hello", 2n],
);
const encodedBsatnInfinities = encodeBsatnProduct(
  [Infinity, -Infinity],
  [
    { name: "f32", kind: "float32" },
    { name: "f64", kind: "float64" },
  ],
);
assert.deepEqual(
  decodeBsatnProduct(
    encodedBsatnInfinities,
    [
      { name: "f32", kind: "float32" },
      { name: "f64", kind: "float64" },
    ],
    (values) => values,
  ),
  [Infinity, -Infinity],
);
assert.throws(
  () => encodeBsatnProduct([Number.NaN], [{ name: "f64", kind: "float64" }]),
  ShunterValidationError,
);
assert.deepEqual(
  decodeBsatnProduct(
    bytesFromHex(
      "000103feff06040302010c02000000dead120200000001000000610200000062631300112233445566778899aabbccddeeff15070000007b2261223a317d",
    ),
    [
      { name: "active", kind: "bool" },
      { name: "count", kind: "int16" },
      { name: "mask", kind: "uint32" },
      { name: "payload", kind: "bytes" },
      { name: "tags", kind: "arrayString" },
      { name: "owner", kind: "uuid" },
      { name: "metadata", kind: "json" },
    ],
    (values) => values,
  ),
  [
    true,
    -2,
    0x01020304,
    new Uint8Array([0xde, 0xad]),
    ["a", "bc"],
    "00112233-4455-6677-8899-aabbccddeeff",
    { a: 1 },
  ],
);
assert.throws(
  () => decodeBsatnProduct(bytesFromHex("070100000000000000"), [{ name: "id", kind: "uint64" }], (values) => values),
  ShunterValidationError,
);
assert.throws(
  () => decodeBsatnProduct(bytesFromHex("0b02"), [{ name: "topic", kind: "string", nullable: true }], (values) => values),
  ShunterValidationError,
);

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
const rawReducerArgs = new Uint8Array([0x01, 0x02]);
const clonedReducerArgs = encodeReducerArgs(rawReducerArgs);
rawReducerArgs[0] = 0xff;
assert.deepEqual(clonedReducerArgs, new Uint8Array([0x01, 0x02]));
assert.deepEqual(
  encodeReducerArgs({ body: "hello" }, (args) => new TextEncoder().encode(args.body)),
  new TextEncoder().encode("hello"),
);
assert.throws(
  () => encodeReducerArgs({ body: "hello" }),
  ShunterValidationError,
);
assert.throws(
  () => encodeReducerArgs({ body: "hello" }, () => "hello"),
  ShunterValidationError,
);

assert.throws(
  () => encodeReducerCallRequest("send", new Uint8Array(), { requestId: 0x1_0000_0000 }),
  ShunterValidationError,
);

assert.throws(
  () => encodeReducerCallRequest("\ud800", new Uint8Array(), { requestId: 1 }),
  ShunterValidationError,
);

const encodedDeclaredQuery = encodeDeclaredQueryRequest("recent_users", {
  messageId: new Uint8Array([0x09, 0x08]),
});
assert.equal(encodedDeclaredQuery.name, "recent_users");
assert.deepEqual(encodedDeclaredQuery.messageId, new Uint8Array([0x09, 0x08]));
assert.equal(encodedDeclaredQuery.frame[0], SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY);
assert.deepEqual(
  encodedDeclaredQuery.frame,
  bytesFromHex("070200000009080c000000726563656e745f7573657273"),
);

const encodedSubscribeSingle = encodeSubscribeSingleRequest("SELECT * FROM users", {
  requestId: 0x01020304,
  queryId: 0x05060708,
});
assert.equal(encodedSubscribeSingle.frame[0], SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_SINGLE);
assert.deepEqual(
  encodedSubscribeSingle.frame,
  bytesFromHex("0104030201080706051300000053454c454354202a2046524f4d207573657273"),
);

const encodedTableSubscription = encodeTableSubscriptionRequest("users", {
  requestId: 0x01020304,
  queryId: 0x05060708,
});
assert.equal(encodedTableSubscription.table, "users");
assert.equal(encodedTableSubscription.queryString, 'SELECT * FROM "users"');

const encodedDeclaredView = encodeDeclaredViewSubscriptionRequest("live_users", {
  requestId: 0x81828384,
  queryId: 0x91929394,
});
assert.equal(encodedDeclaredView.name, "live_users");
assert.equal(encodedDeclaredView.frame[0], SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW);
assert.deepEqual(
  encodedDeclaredView.frame,
  bytesFromHex("0884838281949392910a0000006c6976655f7573657273"),
);

const encodedUnsubscribeSingle = encodeUnsubscribeSingleRequest(0x21222324, {
  requestId: 0x11121314,
});
assert.equal(encodedUnsubscribeSingle.frame[0], SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_SINGLE);
assert.deepEqual(encodedUnsubscribeSingle.frame, bytesFromHex("021413121124232221"));

const encodedUnsubscribe = encodeUnsubscribeMultiRequest(0x71727374, {
  requestId: 0x61626364,
});
assert.equal(encodedUnsubscribe.frame[0], SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_MULTI);
assert.deepEqual(encodedUnsubscribe.frame, bytesFromHex("066463626174737271"));

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
assert.deepEqual(
  committedUpdate.status.updates[0].inserts,
  decodeRowList(committedUpdate.status.updates[0].inserts).rawBytes,
);
assert.deepEqual(
  decodeRowList(committedUpdate.status.updates[0].inserts).rows.map((row) => [...row]),
  [[1, 2], [3]],
);
assert.deepEqual(committedUpdate.status.updates[0].insertRowBytes.map((row) => [...row]), [[1, 2], [3]]);
assert.equal(committedUpdate.status.updates[0].deleteRowBytes, undefined);
assert.equal(committedUpdate.timestamp, 0x0102030405060708n);
assert.deepEqual([...committedUpdate.callerIdentity.slice(0, 3)], [0x20, 0x21, 0x22]);
assert.deepEqual([...committedUpdate.callerConnectionId.slice(0, 3)], [0xa0, 0xa1, 0xa2]);
assert.equal(committedUpdate.reducerCall.name, "send");
assert.equal(committedUpdate.reducerCall.reducerId, 0x11121314);
assert.deepEqual(committedUpdate.reducerCall.args, new Uint8Array([0xaa, 0xbb]));
assert.equal(committedUpdate.reducerCall.requestId, 0x21222324);
assert.equal(committedUpdate.totalHostExecutionDuration, 0x3132333435363738n);
assert.deepEqual(committedUpdate.rawFrame, committedUpdateFrame);
const committedReducerResult = decodeReducerCallResult("send", committedUpdateFrame, {
  requestId: 0x21222324,
});
assert.equal(committedReducerResult.name, "send");
assert.equal(committedReducerResult.requestId, 0x21222324);
assert.equal(committedReducerResult.status, "committed");
assert.deepEqual(committedReducerResult.value, committedUpdateFrame);
assert.deepEqual(committedReducerResult.rawResult, committedUpdateFrame);
const decodedReducerResult = decodeReducerCallResult("send", committedUpdateFrame, {
  decodeResult: (update) => update.reducerCall.args,
});
assert.deepEqual(decodedReducerResult.value, new Uint8Array([0xaa, 0xbb]));
const wrappedReducerResult = await callReducerWithResult(
  async (name, args, options) => {
    assert.equal(name, "send");
    assert.deepEqual(args, new Uint8Array([0xaa]));
    assert.equal(options.requestId, 0x21222324);
    assert.equal(options.noSuccessNotify, undefined);
    return committedUpdateFrame;
  },
  "send",
  new Uint8Array([0xaa]),
  { requestId: 0x21222324 },
);
assert.equal(wrappedReducerResult.status, "committed");
assert.deepEqual(wrappedReducerResult.rawResult, committedUpdateFrame);
const encodedArgsReducerResult = await callReducerWithEncodedArgs(
  async (name, args, options) => {
    assert.equal(name, "send");
    assert.deepEqual(args, new TextEncoder().encode("hello"));
    assert.equal(options.noSuccessNotify, true);
    return args;
  },
  "send",
  { body: "hello" },
  {
    noSuccessNotify: true,
    encodeArgs: (args) => new TextEncoder().encode(args.body),
  },
);
assert.deepEqual(encodedArgsReducerResult, new TextEncoder().encode("hello"));
const encodedArgsWrappedReducerResult = await callReducerWithEncodedArgsResult(
  async (name, args, options) => {
    assert.equal(name, "send");
    assert.deepEqual(args, new TextEncoder().encode("hello"));
    assert.equal(options.requestId, 0x21222324);
    assert.equal(options.noSuccessNotify, undefined);
    return committedUpdateFrame;
  },
  "send",
  { body: "hello" },
  {
    requestId: 0x21222324,
    encodeArgs: (args) => new TextEncoder().encode(args.body),
  },
);
assert.equal(encodedArgsWrappedReducerResult.status, "committed");
assert.throws(
  () => decodeReducerCallResult("other", committedUpdateFrame),
  ShunterProtocolError,
);

const failedUpdateFrame = bytesFromHex(
  "050104000000626f6f6d18171615141312110000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000400000073656e640000000000000000242322210000000000000000",
);
const failedUpdate = decodeTransactionUpdateFrame(failedUpdateFrame);
assert.equal(failedUpdate.status.status, "failed");
assert.equal(failedUpdate.status.error, "boom");
assert.equal(failedUpdate.reducerCall.name, "send");
assert.equal(failedUpdate.reducerCall.requestId, 0x21222324);
const failedReducerResult = decodeReducerCallResult("send", failedUpdateFrame);
assert.equal(failedReducerResult.status, "failed");
assert.equal(failedReducerResult.error.kind, "validation");
assert.equal(failedReducerResult.error.code, "reducer_failed");

const lightUpdateFrame = bytesFromHex(
  "083433323101000000040302010500000075736572730f000000020000000200000001020100000003020000000405",
);
const lightUpdate = decodeTransactionUpdateLightFrame(lightUpdateFrame);
assert.equal(lightUpdateFrame[0], SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT);
assert.equal(lightUpdate.requestId, 0x31323334);
assert.equal(lightUpdate.updates.length, 1);
assert.equal(lightUpdate.updates[0].queryId, 0x01020304);
assert.equal(lightUpdate.updates[0].tableName, "users");
assert.deepEqual(
  decodeRowList(lightUpdate.updates[0].inserts).rows.map((row) => [...row]),
  [[1, 2], [3]],
);
assert.deepEqual(lightUpdate.updates[0].insertRowBytes.map((row) => [...row]), [[1, 2], [3]]);
assert.equal(lightUpdate.updates[0].deleteRowBytes, undefined);

const rowListDeleteLightUpdate = decodeTransactionUpdateLightFrame(bytesFromHex(
  "0801000000010000000200000005000000757365727304000000000000000a00000001000000020000000405",
));
assert.deepEqual(rowListDeleteLightUpdate.updates[0].insertRowBytes.map((row) => [...row]), []);
assert.deepEqual(rowListDeleteLightUpdate.updates[0].deleteRowBytes.map((row) => [...row]), [[4, 5]]);

const oneOffSuccessFrame = bytesFromHex(
  "0602000000010200010000000500000075736572730f0000000200000002000000010201000000031817161514131211",
);
const oneOffSuccess = decodeOneOffQueryResponseFrame(oneOffSuccessFrame);
assert.equal(oneOffSuccessFrame[0], SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE);
assert.deepEqual(oneOffSuccess.messageId, new Uint8Array([0x01, 0x02]));
assert.equal(oneOffSuccess.error, undefined);
assert.equal(oneOffSuccess.tables.length, 1);
assert.equal(oneOffSuccess.tables[0].tableName, "users");
assert.deepEqual([...oneOffSuccess.tables[0].rows.slice(0, 4)], [0x02, 0x00, 0x00, 0x00]);
assert.deepEqual(oneOffSuccess.tables[0].rowBytes.map((row) => [...row]), [[1, 2], [3]]);
assert.equal(oneOffSuccess.totalHostExecutionDuration, 0x1112131415161718n);
const rawDeclaredQueryResult = decodeRawDeclaredQueryResult("recent_users", oneOffSuccessFrame);
assert.equal(rawDeclaredQueryResult.name, "recent_users");
assert.deepEqual(rawDeclaredQueryResult.messageId, new Uint8Array([0x01, 0x02]));
assert.equal(rawDeclaredQueryResult.tables[0].tableName, "users");
assert.deepEqual(rawDeclaredQueryResult.tables[0].rowBytes.map((row) => [...row]), [[1, 2], [3]]);
assert.deepEqual(rawDeclaredQueryResult.rawFrame, oneOffSuccessFrame);
const decodedDeclaredQueryResult = decodeDeclaredQueryResult("recent_users", oneOffSuccessFrame, {
  tableDecoders: {
    users: (row) => [...row].join("-"),
  },
});
assert.equal(decodedDeclaredQueryResult.name, "recent_users");
assert.deepEqual(decodedDeclaredQueryResult.tables[0].rows, ["1-2", "3"]);
assert.deepEqual(decodedDeclaredQueryResult.tables[0].rawRows, rawDeclaredQueryResult.tables[0].rows);
assert.deepEqual(decodedDeclaredQueryResult.tables[0].rowBytes.map((row) => [...row]), [[1, 2], [3]]);
const fallbackDecodedDeclaredQueryResult = decodeDeclaredQueryResult("recent_users", oneOffSuccessFrame, {
  decodeRow: (tableName, row) => `${tableName}:${[...row].join("-")}`,
});
assert.deepEqual(fallbackDecodedDeclaredQueryResult.tables[0].rows, ["users:1-2", "users:3"]);
assert.throws(
  () => decodeDeclaredQueryResult("recent_users", oneOffSuccessFrame, { tableDecoders: {} }),
  ShunterValidationError,
);

const oneOffErrorFrame = bytesFromHex(
  "060200000003040109000000626164207175657279000000002827262524232221",
);
const oneOffError = decodeOneOffQueryResponseFrame(oneOffErrorFrame);
assert.deepEqual(oneOffError.messageId, new Uint8Array([0x03, 0x04]));
assert.equal(oneOffError.error, "bad query");
assert.equal(oneOffError.tables.length, 0);
assert.throws(
  () => decodeRawDeclaredQueryResult("recent_users", oneOffErrorFrame),
  ShunterValidationError,
);

const subscribeSingleAppliedFrame = bytesFromHex(
  "02040302010807060504030201141312110500000075736572730f000000020000000200000001020100000003",
);
const subscribeSingleApplied = decodeSubscribeSingleAppliedFrame(subscribeSingleAppliedFrame);
assert.equal(subscribeSingleAppliedFrame[0], SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED);
assert.equal(subscribeSingleApplied.requestId, 0x01020304);
assert.equal(subscribeSingleApplied.queryId, 0x11121314);
assert.equal(subscribeSingleApplied.tableName, "users");
assert.deepEqual([...subscribeSingleApplied.rows.slice(0, 4)], [0x02, 0x00, 0x00, 0x00]);
assert.deepEqual(subscribeSingleApplied.rowBytes.map((row) => [...row]), [[1, 2], [3]]);

const unsubscribeSingleAppliedFrame = bytesFromHex(
  "0324232221181716151413121134333231010f000000020000000200000001020100000003",
);
const unsubscribeSingleApplied = decodeUnsubscribeSingleAppliedFrame(unsubscribeSingleAppliedFrame);
assert.equal(unsubscribeSingleAppliedFrame[0], SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED);
assert.equal(unsubscribeSingleApplied.requestId, 0x21222324);
assert.equal(unsubscribeSingleApplied.queryId, 0x31323334);
assert.equal(unsubscribeSingleApplied.hasRows, true);
assert.deepEqual([...unsubscribeSingleApplied.rows.slice(0, 4)], [0x02, 0x00, 0x00, 0x00]);
assert.deepEqual(unsubscribeSingleApplied.rowBytes.map((row) => [...row]), [[1, 2], [3]]);

const unsubscribeSingleAppliedWithoutRows = decodeUnsubscribeSingleAppliedFrame(
  bytesFromHex("032423222118171615141312113433323100"),
);
assert.equal(unsubscribeSingleAppliedWithoutRows.hasRows, false);
assert.equal(unsubscribeSingleAppliedWithoutRows.rows, undefined);
assert.equal(unsubscribeSingleAppliedWithoutRows.rowBytes, undefined);

assert.deepEqual(decodeRowList(bytesFromHex("00000000")).rows, []);
assert.throws(
  () => decodeRowList(bytesFromHex("02000000020000000102")),
  ShunterProtocolError,
);

const subscribeAppliedFrame = bytesFromHex(
  "094443424158575655545352516463626101000000040302010500000075736572730f000000020000000200000001020100000003020000000405",
);
const subscribeApplied = decodeSubscribeMultiAppliedFrame(subscribeAppliedFrame);
assert.equal(subscribeAppliedFrame[0], SHUNTER_SERVER_MESSAGE_SUBSCRIBE_MULTI_APPLIED);
assert.equal(subscribeApplied.requestId, 0x41424344);
assert.equal(subscribeApplied.queryId, 0x61626364);
assert.equal(subscribeApplied.totalHostExecutionDurationMicros, 0x5152535455565758n);
assert.equal(subscribeApplied.updates.length, 1);
assert.equal(subscribeApplied.updates[0].tableName, "users");
assert.deepEqual(subscribeApplied.updates[0].insertRowBytes.map((row) => [...row]), [[1, 2], [3]]);
assert.equal(subscribeApplied.updates[0].deleteRowBytes, undefined);

const unsubscribeMultiAppliedFrame = bytesFromHex(
  "0a7473727188878685848382819493929101000000040302010500000075736572730f000000020000000200000001020100000003020000000405",
);
const unsubscribeMultiApplied = decodeUnsubscribeMultiAppliedFrame(unsubscribeMultiAppliedFrame);
assert.equal(unsubscribeMultiAppliedFrame[0], SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED);
assert.equal(unsubscribeMultiApplied.requestId, 0x71727374);
assert.equal(unsubscribeMultiApplied.queryId, 0x91929394);
assert.equal(unsubscribeMultiApplied.updates.length, 1);

const unsubscribeDeclaredViewAppliedFrame = bytesFromHex(
  "0a0100000000000000000000006463626100000000",
);

const subscriptionErrorFrame = bytesFromHex(
  "0408070605040302010144434241015453525101646362610600000064656e696564",
);
const reconnectSubscriptionErrorFrame = bytesFromHex(
  "04000000000000000001010000000114131211000d0000007265706c61792064656e696564",
);
const subscriptionError = decodeSubscriptionErrorFrame(subscriptionErrorFrame);
assert.equal(subscriptionErrorFrame[0], SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR);
assert.equal(subscriptionError.requestId, 0x41424344);
assert.equal(subscriptionError.queryId, 0x51525354);
assert.equal(subscriptionError.tableId, 0x61626364);
assert.equal(subscriptionError.error, "denied");

const clientStates = [];
const encodedToken = "space token&equals=value/slash?";
const tokenQuerySockets = [];
const tokenQueryClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe?token=old-token&existing=1&token=stale-token",
  protocol: shunterProtocol,
  token: encodedToken,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    tokenQuerySockets.push(socket);
    return socket;
  },
});
const tokenQueryConnecting = tokenQueryClient.connect();
await nextTurn();
const tokenQueryUrl = new URL(tokenQuerySockets[0].url);
assert.deepEqual(tokenQueryUrl.searchParams.getAll("token"), [encodedToken]);
assert.equal(tokenQueryUrl.searchParams.get("existing"), "1");
assert.match(tokenQuerySockets[0].url, /token=space\+token%26equals%3Dvalue%2Fslash%3F/);
assert(!tokenQuerySockets[0].url.includes("old-token"));
assert(!tokenQuerySockets[0].url.includes("stale-token"));
tokenQuerySockets[0].open();
tokenQuerySockets[0].message(identityTokenFrame().buffer);
await tokenQueryConnecting;
await tokenQueryClient.close();

const asyncTokenSockets = [];
let resolveAsyncToken;
let asyncTokenCalls = 0;
const asyncTokenClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe?existing=1",
  protocol: shunterProtocol,
  token: () => {
    asyncTokenCalls += 1;
    return new Promise((resolve) => {
      resolveAsyncToken = resolve;
    });
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    asyncTokenSockets.push(socket);
    return socket;
  },
});
const asyncTokenConnecting = asyncTokenClient.connect();
await nextTurn();
assert.equal(asyncTokenCalls, 1);
assert.equal(asyncTokenClient.state.status, "connecting");
assert.equal(asyncTokenSockets.length, 0);
resolveAsyncToken("async token&value");
await nextTurn();
assert.equal(asyncTokenSockets.length, 1);
const asyncTokenUrl = new URL(asyncTokenSockets[0].url);
assert.deepEqual(asyncTokenUrl.searchParams.getAll("token"), ["async token&value"]);
assert.equal(asyncTokenUrl.searchParams.get("existing"), "1");
asyncTokenSockets[0].open();
asyncTokenSockets[0].message(identityTokenFrame().buffer);
await asyncTokenConnecting;
await asyncTokenClient.close();

let asyncTokenFailureFactoryCalls = 0;
const asyncTokenFailureClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => Promise.reject(new Error("async token denied")),
  webSocketFactory: () => {
    asyncTokenFailureFactoryCalls += 1;
    throw new Error("should not create socket");
  },
});
await assert.rejects(asyncTokenFailureClient.connect(), (error) => {
  assert(error instanceof ShunterAuthError);
  assert.equal(error.kind, "auth");
  assert.match(error.message, /Token provider failed/);
  return true;
});
assert.equal(asyncTokenFailureClient.state.status, "failed");
assert.equal(asyncTokenFailureFactoryCalls, 0);

const preAbortedConnect = new AbortController();
preAbortedConnect.abort();
let preAbortedConnectFactoryCalls = 0;
const preAbortedConnectClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  signal: preAbortedConnect.signal,
  webSocketFactory: () => {
    preAbortedConnectFactoryCalls += 1;
    throw new Error("should not create socket");
  },
});
await assert.rejects(preAbortedConnectClient.connect(), (error) => {
  assert(error instanceof ShunterClosedClientError);
  assert.equal(error.kind, "closed");
  assert.match(error.message, /Connection aborted before opening/);
  return true;
});
assert.equal(preAbortedConnectClient.state.status, "failed");
assert.equal(preAbortedConnectFactoryCalls, 0);

const asyncAbortConnect = new AbortController();
let resolveAsyncAbortToken;
let asyncAbortConnectFactoryCalls = 0;
const asyncAbortConnectClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  signal: asyncAbortConnect.signal,
  token: () => new Promise((resolve) => {
    resolveAsyncAbortToken = resolve;
  }),
  webSocketFactory: () => {
    asyncAbortConnectFactoryCalls += 1;
    throw new Error("should not create socket");
  },
});
const asyncAbortConnecting = asyncAbortConnectClient.connect();
await nextTurn();
asyncAbortConnect.abort();
resolveAsyncAbortToken("too-late");
await assert.rejects(asyncAbortConnecting, ShunterClosedClientError);
assert.equal(asyncAbortConnectClient.state.status, "failed");
assert.equal(asyncAbortConnectFactoryCalls, 0);

const closePendingTokenSockets = [];
let resolveClosePendingToken;
let closePendingTokenCalls = 0;
const closePendingTokenClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    closePendingTokenCalls += 1;
    return new Promise((resolve) => {
      resolveClosePendingToken = resolve;
    });
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    closePendingTokenSockets.push(socket);
    return socket;
  },
});
const closePendingTokenConnecting = closePendingTokenClient.connect();
await nextTurn();
assert.equal(closePendingTokenCalls, 1);
assert.equal(closePendingTokenClient.state.status, "connecting");
const closePendingTokenClosed = closePendingTokenClient.close(4002, "caller closed before token");
await assert.rejects(closePendingTokenConnecting, ShunterClosedClientError);
await closePendingTokenClosed;
assert.equal(closePendingTokenClient.state.status, "closed");
assert.equal(closePendingTokenSockets.length, 0);
resolveClosePendingToken("too-late");
await nextTurn();
assert.equal(closePendingTokenSockets.length, 0);
assert.equal(closePendingTokenClient.state.status, "closed");

const rejectAfterClosePendingTokenSockets = [];
let rejectAfterClosePendingToken;
const rejectAfterClosePendingTokenClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => new Promise((_, reject) => {
    rejectAfterClosePendingToken = reject;
  }),
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    rejectAfterClosePendingTokenSockets.push(socket);
    return socket;
  },
});
const rejectAfterClosePendingTokenConnecting = rejectAfterClosePendingTokenClient.connect();
await nextTurn();
assert.equal(rejectAfterClosePendingTokenClient.state.status, "connecting");
const rejectAfterClosePendingTokenClosed = rejectAfterClosePendingTokenClient.close(4002, "caller closed before token");
await assert.rejects(rejectAfterClosePendingTokenConnecting, ShunterClosedClientError);
await rejectAfterClosePendingTokenClosed;
assert.equal(rejectAfterClosePendingTokenClient.state.status, "closed");
rejectAfterClosePendingToken(new Error("too-late"));
await nextTurn();
assert.equal(rejectAfterClosePendingTokenSockets.length, 0);
assert.equal(rejectAfterClosePendingTokenClient.state.status, "closed");

const reconnectWhileOldTokenPendingSockets = [];
const reconnectWhileOldTokenPendingResolvers = [];
let reconnectWhileOldTokenPendingCalls = 0;
const reconnectWhileOldTokenPendingClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    reconnectWhileOldTokenPendingCalls += 1;
    return new Promise((resolve) => {
      reconnectWhileOldTokenPendingResolvers.push(resolve);
    });
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectWhileOldTokenPendingSockets.push(socket);
    return socket;
  },
});
const staleTokenConnect = reconnectWhileOldTokenPendingClient.connect();
await nextTurn();
const staleTokenClosed = reconnectWhileOldTokenPendingClient.close(4002, "caller closed before token");
await assert.rejects(
  staleTokenConnect,
  ShunterClosedClientError,
);
await staleTokenClosed;
assert.equal(reconnectWhileOldTokenPendingClient.state.status, "closed");
const freshTokenConnect = reconnectWhileOldTokenPendingClient.connect();
await nextTurn();
assert.equal(reconnectWhileOldTokenPendingCalls, 2);
reconnectWhileOldTokenPendingResolvers[0]("stale-token");
await nextTurn();
assert.equal(reconnectWhileOldTokenPendingSockets.length, 0);
assert.equal(reconnectWhileOldTokenPendingClient.state.status, "connecting");
reconnectWhileOldTokenPendingResolvers[1]("fresh-token");
await nextTurn();
assert.equal(reconnectWhileOldTokenPendingSockets.length, 1);
assert.equal(reconnectWhileOldTokenPendingSockets[0].url, "ws://127.0.0.1:3000/subscribe?token=fresh-token");
reconnectWhileOldTokenPendingSockets[0].open();
reconnectWhileOldTokenPendingSockets[0].message(identityTokenFrame({ token: "fresh-identity" }).buffer);
const freshTokenMetadata = await freshTokenConnect;
assert.equal(freshTokenMetadata.identityToken, "fresh-identity");
assert.equal(reconnectWhileOldTokenPendingClient.state.status, "connected");
await reconnectWhileOldTokenPendingClient.close();

const disposePendingTokenSockets = [];
let resolveDisposePendingToken;
let disposePendingTokenCalls = 0;
const disposePendingTokenClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    disposePendingTokenCalls += 1;
    return new Promise((resolve) => {
      resolveDisposePendingToken = resolve;
    });
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    disposePendingTokenSockets.push(socket);
    return socket;
  },
});
const disposePendingTokenConnecting = disposePendingTokenClient.connect();
await nextTurn();
assert.equal(disposePendingTokenCalls, 1);
assert.equal(disposePendingTokenClient.state.status, "connecting");
const disposePendingTokenClosed = disposePendingTokenClient.dispose();
await assert.rejects(disposePendingTokenConnecting, ShunterClosedClientError);
await disposePendingTokenClosed;
assert.equal(disposePendingTokenClient.state.status, "closed");
assert.equal(disposePendingTokenSockets.length, 0);
resolveDisposePendingToken("too-late");
await nextTurn();
assert.equal(disposePendingTokenSockets.length, 0);
assert.equal(disposePendingTokenClient.state.status, "closed");
await assert.rejects(disposePendingTokenClient.connect(), ShunterClosedClientError);

const abortHandshake = new AbortController();
const abortHandshakeSockets = [];
const abortHandshakeStates = [];
const abortHandshakeClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  signal: abortHandshake.signal,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    abortHandshakeSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => abortHandshakeStates.push(current.status),
});
const abortHandshakeConnecting = abortHandshakeClient.connect();
await nextTurn();
assert.equal(abortHandshakeSockets.length, 1);
abortHandshakeSockets[0].open();
abortHandshake.abort();
await rejectByNextTurn(abortHandshakeConnecting, (error) => {
  assert(error instanceof ShunterClosedClientError);
  assert.equal(error.kind, "closed");
  assert.match(error.message, /Connection aborted before opening/);
});
assert.equal(abortHandshakeClient.state.status, "failed");
assert.deepEqual(abortHandshakeSockets[0].closeCalls, [{ code: 1000, reason: "connection aborted" }]);
assert.deepEqual(abortHandshakeStates, ["connecting", "failed"]);

const reconnectTokenSockets = [];
let reconnectTokenCallsForFailure = 0;
const reconnectTokenClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe?token=old-token&existing=1&token=stale-token",
  protocol: shunterProtocol,
  token: () => {
    reconnectTokenCallsForFailure += 1;
    return `retry-token-${reconnectTokenCallsForFailure}`;
  },
  reconnect: {
    enabled: true,
    maxAttempts: 2,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectTokenSockets.push(socket);
    return socket;
  },
});
const reconnectTokenConnecting = reconnectTokenClient.connect();
await nextTurn();
assertSingleTokenUrl(reconnectTokenSockets[0].url, "retry-token-1");
reconnectTokenSockets[0].open();
reconnectTokenSockets[0].message(identityTokenFrame().buffer);
await reconnectTokenConnecting;
reconnectTokenSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
await nextTurn();
assert.equal(reconnectTokenClient.state.status, "connecting");
assert.equal(reconnectTokenCallsForFailure, 2);
assert.equal(reconnectTokenSockets.length, 2);
assertSingleTokenUrl(reconnectTokenSockets[1].url, "retry-token-2");
reconnectTokenSockets[1].open();
reconnectTokenSockets[1].dispatch("close", {
  code: 1006,
  reason: "first retry failed",
  wasClean: false,
});
await nextTurn();
assert.equal(reconnectTokenClient.state.status, "connecting");
assert.equal(reconnectTokenCallsForFailure, 3);
assert.equal(reconnectTokenSockets.length, 3);
assertSingleTokenUrl(reconnectTokenSockets[2].url, "retry-token-3");
const observedTokenReconnect = reconnectTokenClient.connect();
reconnectTokenSockets[2].open();
reconnectTokenSockets[2].dispatch("close", {
  code: 1006,
  reason: "second retry failed",
  wasClean: false,
});
await rejectByNextTurn(observedTokenReconnect, (error) => {
  assert(error instanceof ShunterTransportError);
  assert.equal(error.code, "1006");
  assert.deepEqual(error.details, { reason: "second retry failed", wasClean: false });
});
assert.equal(reconnectTokenClient.state.status, "closed");
assert.equal(reconnectTokenCallsForFailure, 3);

const reconnectAsyncTokenSockets = [];
const reconnectAsyncTokenResolvers = [];
let reconnectAsyncTokenCalls = 0;
const reconnectAsyncTokenClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe?existing=1",
  protocol: shunterProtocol,
  token: () => {
    reconnectAsyncTokenCalls += 1;
    return new Promise((resolve) => {
      reconnectAsyncTokenResolvers.push(resolve);
    });
  },
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectAsyncTokenSockets.push(socket);
    return socket;
  },
});
const reconnectAsyncTokenConnecting = reconnectAsyncTokenClient.connect();
await nextTurn();
assert.equal(reconnectAsyncTokenCalls, 1);
assert.equal(reconnectAsyncTokenSockets.length, 0);
reconnectAsyncTokenResolvers[0]("async-retry-token-1");
await nextTurn();
assertSingleTokenUrl(reconnectAsyncTokenSockets[0].url, "async-retry-token-1");
reconnectAsyncTokenSockets[0].open();
reconnectAsyncTokenSockets[0].message(identityTokenFrame().buffer);
await reconnectAsyncTokenConnecting;
const reconnectAsyncTokenHandleSubscription = reconnectAsyncTokenClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectAsyncTokenSockets[0].message(subscribeSingleAppliedFrame);
const reconnectAsyncTokenHandle = await reconnectAsyncTokenHandleSubscription;
assert.deepEqual(reconnectAsyncTokenHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectAsyncTokenSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectAsyncTokenClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectAsyncTokenCalls, 2);
assert.equal(reconnectAsyncTokenSockets.length, 1);
reconnectAsyncTokenResolvers[1]("async-retry-token-2");
await nextTurn();
assert.equal(reconnectAsyncTokenSockets.length, 2);
assertSingleTokenUrl(reconnectAsyncTokenSockets[1].url, "async-retry-token-2");
reconnectAsyncTokenSockets[1].open();
reconnectAsyncTokenSockets[1].message(identityTokenFrame().buffer);
assert.equal(reconnectAsyncTokenClient.state.status, "connected");
assert.deepEqual(
  reconnectAsyncTokenSockets[1].sent[0],
  encodeTableSubscriptionRequest("users", {
    requestId: 1,
    queryId: 0x11121314,
  }).frame,
);
reconnectAsyncTokenSockets[1].message(bytesFromHex(
  "02010000000000000000000000141312110500000075736572730a00000001000000020000000405",
));
assert.deepEqual(reconnectAsyncTokenHandle.state, { status: "active", rows: ["4-5"] });
await reconnectAsyncTokenClient.close();

const manualReconnectSockets = [];
const manualReconnectClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 20,
    maxDelayMs: 20,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    manualReconnectSockets.push(socket);
    return socket;
  },
});
const manualReconnectConnecting = manualReconnectClient.connect();
await nextTurn();
manualReconnectSockets[0].open();
manualReconnectSockets[0].message(identityTokenFrame().buffer);
await manualReconnectConnecting;
manualReconnectSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(manualReconnectClient.state.status, "reconnecting");
const manualReconnectRetry = manualReconnectClient.connect();
await nextTurn();
assert.equal(manualReconnectClient.state.status, "connecting");
assert.equal(manualReconnectSockets.length, 2);
manualReconnectSockets[1].open();
manualReconnectSockets[1].message(identityTokenFrame({ token: "manual-reconnect-token" }).buffer);
const manualReconnectMetadata = await manualReconnectRetry;
assert.equal(manualReconnectMetadata.identityToken, "manual-reconnect-token");
assert.equal(manualReconnectClient.state.status, "connected");
await new Promise((resolve) => setTimeout(resolve, 30));
assert.equal(manualReconnectSockets.length, 2);
await manualReconnectClient.close();

const client = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe?existing=1",
  protocol: shunterProtocol,
  token: "test-token",
  webSocketFactory: fakeFactory,
  onStateChange: ({ current }) => clientStates.push(current.status),
});
const connecting = client.connect();
await nextTurn();
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

const concurrentConnectSockets = [];
const concurrentConnectClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    concurrentConnectSockets.push(socket);
    return socket;
  },
});
const firstConcurrentConnect = concurrentConnectClient.connect();
const secondConcurrentConnect = concurrentConnectClient.connect();
await nextTurn();
assert.equal(concurrentConnectSockets.length, 1);
concurrentConnectSockets[0].open();
concurrentConnectSockets[0].message(identityTokenFrame({ token: "concurrent-token" }).buffer);
const firstConcurrentMetadata = await firstConcurrentConnect;
const secondConcurrentMetadata = await secondConcurrentConnect;
assert.strictEqual(firstConcurrentMetadata, secondConcurrentMetadata);
assert.equal(firstConcurrentMetadata.identityToken, "concurrent-token");
assert.strictEqual(await concurrentConnectClient.connect(), firstConcurrentMetadata);
await concurrentConnectClient.close();

const reconnectAfterCloseSockets = [];
const reconnectAfterCloseStates = [];
const reconnectAfterCloseClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectAfterCloseSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectAfterCloseStates.push(current.status),
});
const firstReconnectAfterClose = reconnectAfterCloseClient.connect();
await nextTurn();
reconnectAfterCloseSockets[0].open();
reconnectAfterCloseSockets[0].message(identityTokenFrame({ token: "first-close-token" }).buffer);
const firstReconnectAfterCloseMetadata = await firstReconnectAfterClose;
assert.equal(firstReconnectAfterCloseMetadata.identityToken, "first-close-token");
await reconnectAfterCloseClient.close();
assert.equal(reconnectAfterCloseClient.state.status, "closed");
const secondReconnectAfterClose = reconnectAfterCloseClient.connect();
await nextTurn();
assert.equal(reconnectAfterCloseSockets.length, 2);
reconnectAfterCloseSockets[1].open();
reconnectAfterCloseSockets[1].message(identityTokenFrame({ token: "second-close-token" }).buffer);
const secondReconnectAfterCloseMetadata = await secondReconnectAfterClose;
assert.equal(secondReconnectAfterCloseMetadata.identityToken, "second-close-token");
assert.equal(reconnectAfterCloseClient.state.status, "connected");
assert.deepEqual(reconnectAfterCloseStates, [
  "connecting",
  "connected",
  "closing",
  "closed",
  "connecting",
  "connected",
]);
await reconnectAfterCloseClient.close();

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

const declaredQueryResponse = client.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x01, 0x02]),
});
assert.equal(sockets[0].sent.length, 4);
assert.deepEqual(
  sockets[0].sent[3],
  encodeDeclaredQueryRequest("recent_users", { messageId: new Uint8Array([0x01, 0x02]) }).frame,
);
sockets[0].message(oneOffSuccessFrame);
assert.deepEqual(await declaredQueryResponse, oneOffSuccessFrame);

const declaredQueryFailure = client.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x03, 0x04]),
});
assert.equal(sockets[0].sent.length, 5);
sockets[0].message(oneOffErrorFrame);
await assert.rejects(declaredQueryFailure, ShunterValidationError);

const declaredViewRawUpdates = [];
const declaredViewSubscription = client.subscribeDeclaredView("live_users", {
  requestId: 0x41424344,
  queryId: 0x61626364,
  onRawUpdate: (update) => declaredViewRawUpdates.push(update),
});
assert.equal(sockets[0].sent.length, 6);
assert.deepEqual(
  sockets[0].sent[5],
  encodeDeclaredViewSubscriptionRequest("live_users", {
    requestId: 0x41424344,
    queryId: 0x61626364,
  }).frame,
);
sockets[0].message(subscribeAppliedFrame);
const unsubscribeDeclaredView = await declaredViewSubscription;
assert.equal(declaredViewRawUpdates.length, 1);
assert.equal(declaredViewRawUpdates[0].queryId, 0x01020304);
sockets[0].message(lightUpdateFrame);
assert.equal(declaredViewRawUpdates.length, 2);
assert.equal(declaredViewRawUpdates[1].tableName, "users");
const unsubscribeDeclaredViewResult = unsubscribeDeclaredView();
assert.equal(unsubscribeDeclaredView(), unsubscribeDeclaredViewResult);
assert.equal(sockets[0].sent.length, 7);
assert.deepEqual(
  sockets[0].sent[6],
  encodeUnsubscribeMultiRequest(0x61626364, { requestId: 1 }).frame,
);
sockets[0].message(lightUpdateFrame);
assert.equal(declaredViewRawUpdates.length, 3);
sockets[0].message(unsubscribeDeclaredViewAppliedFrame);
await unsubscribeDeclaredViewResult;
sockets[0].message(lightUpdateFrame);
assert.equal(declaredViewRawUpdates.length, 3);

const deniedViewSubscription = client.subscribeDeclaredView("live_users", {
  requestId: 0x41424344,
  queryId: 0x51525354,
});
assert.equal(sockets[0].sent.length, 8);
sockets[0].message(subscriptionErrorFrame);
await assert.rejects(deniedViewSubscription, ShunterValidationError);

const tableRawRows = [];
const tableRawUpdates = [];
const tableInitialRows = [];
const tableOptionInitialRows = [];
const tableDecodedUpdates = [];
const tableLightUpdateFrame = bytesFromHex(
  "083433323101000000141312110500000075736572730f0000000200000002000000010201000000030a00000001000000020000000405",
);
const unsubscribeTableAppliedFrame = bytesFromHex(
  "030200000000000000000000001413121100",
);
const tableSubscription = client.subscribeTable("users", (rows) => tableInitialRows.push(rows), {
  requestId: 0x01020304,
  queryId: 0x11121314,
  decodeRow: (row) => [...row].join("-"),
  onInitialRows: (rows) => tableOptionInitialRows.push(rows),
  onUpdate: (update) => tableDecodedUpdates.push(update),
  onRawRows: (message) => tableRawRows.push(message),
  onRawUpdate: (update) => tableRawUpdates.push(update),
});
assert.equal(sockets[0].sent.length, 9);
assert.deepEqual(
  sockets[0].sent[8],
  encodeTableSubscriptionRequest("users", {
    requestId: 0x01020304,
    queryId: 0x11121314,
  }).frame,
);
sockets[0].message(subscribeSingleAppliedFrame);
const unsubscribeTable = await tableSubscription;
assert.equal(tableRawRows.length, 1);
assert.equal(tableRawRows[0].tableName, "users");
assert.deepEqual(tableInitialRows, [["1-2", "3"]]);
assert.deepEqual(tableOptionInitialRows, [["1-2", "3"]]);
sockets[0].message(tableLightUpdateFrame);
assert.equal(tableRawUpdates.length, 1);
assert.equal(tableRawUpdates[0].queryId, 0x11121314);
assert.deepEqual(tableRawUpdates[0].insertRowBytes.map((row) => [...row]), [[1, 2], [3]]);
assert.deepEqual(tableRawUpdates[0].deleteRowBytes.map((row) => [...row]), [[4, 5]]);
assert.equal(tableDecodedUpdates.length, 1);
assert.equal(tableDecodedUpdates[0].queryId, 0x11121314);
assert.deepEqual(tableDecodedUpdates[0].inserts, ["1-2", "3"]);
assert.deepEqual(tableDecodedUpdates[0].deletes, ["4-5"]);
const unsubscribeTableResult = unsubscribeTable();
assert.equal(unsubscribeTable(), unsubscribeTableResult);
assert.equal(sockets[0].sent.length, 10);
assert.deepEqual(
  sockets[0].sent[9],
  encodeUnsubscribeSingleRequest(0x11121314, { requestId: 2 }).frame,
);
sockets[0].message(tableLightUpdateFrame);
assert.equal(tableRawUpdates.length, 2);
assert.equal(tableDecodedUpdates.length, 2);
sockets[0].message(unsubscribeTableAppliedFrame);
await unsubscribeTableResult;
sockets[0].message(tableLightUpdateFrame);
assert.equal(tableRawUpdates.length, 2);
assert.equal(tableDecodedUpdates.length, 2);

const deniedTableSubscription = client.subscribeTable("users", undefined, {
  requestId: 0x41424344,
  queryId: 0x51525354,
});
assert.equal(sockets[0].sent.length, 11);
sockets[0].message(subscriptionErrorFrame);
await assert.rejects(deniedTableSubscription, ShunterValidationError);

const unsubscribeErrorSubscribeAppliedFrame = bytesFromHex(
  "020403020100000000000000003433323105000000757365727300000000",
);
assert.deepEqual(decodeSubscribeSingleAppliedFrame(unsubscribeErrorSubscribeAppliedFrame).rowBytes, []);
const unsubscribeErrorFrame = bytesFromHex(
  "04000000000000000001030000000134333231000600000064656e696564",
);
const unsubscribeErrorSubscription = client.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x31323334,
});
assert.equal(sockets[0].sent.length, 12);
sockets[0].message(unsubscribeErrorSubscribeAppliedFrame);
const unsubscribeWithError = await unsubscribeErrorSubscription;
const unsubscribeErrorResult = unsubscribeWithError();
assert.equal(sockets[0].sent.length, 13);
assert.deepEqual(
  sockets[0].sent[12],
  encodeUnsubscribeSingleRequest(0x31323334, { requestId: 3 }).frame,
);
sockets[0].message(unsubscribeErrorFrame);
await assert.rejects(unsubscribeErrorResult, ShunterValidationError);

const unsubscribeDeclaredViewHandleAppliedFrame = bytesFromHex(
  "0a0400000000000000000000006463626100000000",
);
const declaredViewHandleSubscription = client.subscribeDeclaredView("live_users", {
  requestId: 0x41424344,
  queryId: 0x61626364,
  returnHandle: true,
});
assert.equal(sockets[0].sent.length, 14);
assert.deepEqual(
  sockets[0].sent[13],
  encodeDeclaredViewSubscriptionRequest("live_users", {
    requestId: 0x41424344,
    queryId: 0x61626364,
  }).frame,
);
sockets[0].message(subscribeAppliedFrame);
const declaredViewHandle = await declaredViewHandleSubscription;
assert.equal(declaredViewHandle.queryId, 0x61626364);
assert.deepEqual(declaredViewHandle.state, { status: "active", rows: [] });
const unsubscribeDeclaredViewHandle = declaredViewHandle.unsubscribe();
void declaredViewHandle.unsubscribe();
assert.equal(sockets[0].sent.length, 15);
assert.deepEqual(
  sockets[0].sent[14],
  encodeUnsubscribeMultiRequest(0x61626364, { requestId: 4 }).frame,
);
sockets[0].message(unsubscribeDeclaredViewHandleAppliedFrame);
await unsubscribeDeclaredViewHandle;
assert.deepEqual(await declaredViewHandle.closed, { reason: "unsubscribed" });
assert.deepEqual(declaredViewHandle.state, { status: "closed" });

const unsubscribeTableHandleAppliedFrame = bytesFromHex(
  "030500000000000000000000001413121100",
);
const tableHandleCacheUpdateFrame = bytesFromHex(
  "083433323101000000141312110500000075736572730a000000010000000200000004050a00000001000000020000000102",
);
const reconnectSubscribeSingleAppliedFrame = bytesFromHex(
  "02010000000000000000000000141312110500000075736572730a00000001000000020000000405",
);
const secondReconnectSubscribeSingleAppliedFrame = new Uint8Array(reconnectSubscribeSingleAppliedFrame);
secondReconnectSubscribeSingleAppliedFrame[1] = 2;
const tableHandleSubscription = client.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
});
assert.equal(sockets[0].sent.length, 16);
assert.deepEqual(
  sockets[0].sent[15],
  encodeTableSubscriptionRequest("users", {
    requestId: 0x01020304,
    queryId: 0x11121314,
  }).frame,
);
sockets[0].message(subscribeSingleAppliedFrame);
const tableHandle = await tableHandleSubscription;
assert.equal(tableHandle.queryId, 0x11121314);
assert.equal(tableHandle.state.status, "active");
assert.deepEqual(tableHandle.state.rows.map((row) => [...row]), [[1, 2], [3]]);
sockets[0].message(tableHandleCacheUpdateFrame);
assert.equal(tableHandle.state.status, "active");
assert.deepEqual(tableHandle.state.rows.map((row) => [...row]), [[3], [4, 5]]);
const unsubscribeTableHandle = tableHandle.unsubscribe();
void tableHandle.unsubscribe();
assert.equal(sockets[0].sent.length, 17);
assert.deepEqual(
  sockets[0].sent[16],
  encodeUnsubscribeSingleRequest(0x11121314, { requestId: 5 }).frame,
);
sockets[0].message(unsubscribeTableHandleAppliedFrame);
await unsubscribeTableHandle;
assert.deepEqual(await tableHandle.closed, { reason: "unsubscribed" });
assert.deepEqual(tableHandle.state, { status: "closed" });

const unsubscribeDecodedTableHandleAppliedFrame = bytesFromHex(
  "030600000000000000000000001413121100",
);
const decodedTableHandleSubscription = client.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
assert.equal(sockets[0].sent.length, 18);
sockets[0].message(subscribeSingleAppliedFrame);
const decodedTableHandle = await decodedTableHandleSubscription;
assert.equal(decodedTableHandle.queryId, 0x11121314);
assert.deepEqual(decodedTableHandle.state, { status: "active", rows: ["1-2", "3"] });
sockets[0].message(tableHandleCacheUpdateFrame);
assert.deepEqual(decodedTableHandle.state, { status: "active", rows: ["3", "4-5"] });
const unsubscribeDecodedTableHandle = decodedTableHandle.unsubscribe();
assert.equal(sockets[0].sent.length, 19);
assert.deepEqual(
  sockets[0].sent[18],
  encodeUnsubscribeSingleRequest(0x11121314, { requestId: 6 }).frame,
);
sockets[0].message(unsubscribeDecodedTableHandleAppliedFrame);
await unsubscribeDecodedTableHandle;
assert.deepEqual(await decodedTableHandle.closed, { reason: "unsubscribed" });
assert.deepEqual(decodedTableHandle.state, { status: "closed" });

await client.close();
await client.close();
assert.equal(sockets[0].closeCalls.length, 1);
assert.deepEqual(clientStates, ["connecting", "connected", "closing", "closed"]);
const assertClosedClientError = (error) => {
  assert(error instanceof ShunterClosedClientError);
  return true;
};
await assert.rejects(
  client.callReducer("send", new Uint8Array(), { requestId: 1 }),
  assertClosedClientError,
);
await assert.rejects(
  client.runDeclaredQuery("recent_users"),
  assertClosedClientError,
);
await assert.rejects(
  client.subscribeDeclaredView("live_users", { returnHandle: true }),
  assertClosedClientError,
);
await assert.rejects(
  client.subscribeTable("users", undefined, { returnHandle: true }),
  assertClosedClientError,
);
await client.dispose();
await assert.rejects(client.connect(), assertClosedClientError);
await assert.rejects(
  client.callReducer("send", new Uint8Array(), { requestId: 1 }),
  assertClosedClientError,
);
await assert.rejects(
  client.runDeclaredQuery("recent_users"),
  assertClosedClientError,
);
await assert.rejects(
  client.subscribeDeclaredView("live_users", { returnHandle: true }),
  assertClosedClientError,
);
await assert.rejects(
  client.subscribeTable("users", undefined, { returnHandle: true }),
  assertClosedClientError,
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
await nextTurn();
const mismatchSocket = sockets.at(-1);
mismatchSocket.open("v1.bsatn.spacetimedb");
await assert.rejects(mismatchConnecting, ShunterProtocolMismatchError);
assert.equal(mismatchClient.state.status, "failed");
assert.equal(mismatchSocket.closeCalls.length, 1);

const missingProtocolClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: fakeFactory,
});
const missingProtocolConnecting = missingProtocolClient.connect();
await nextTurn();
const missingProtocolSocket = sockets.at(-1);
missingProtocolSocket.open("");
await assert.rejects(missingProtocolConnecting, ShunterProtocolMismatchError);
assert.equal(missingProtocolClient.state.status, "failed");
assert.equal(missingProtocolSocket.closeCalls.length, 1);

const malformedClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: fakeFactory,
});
const malformedConnecting = malformedClient.connect();
await nextTurn();
const malformedSocket = sockets.at(-1);
malformedSocket.open();
malformedSocket.message(new Uint8Array([1, 2, 3]));
await assert.rejects(malformedConnecting, ShunterProtocolError);
assert.equal(malformedClient.state.status, "failed");

const cleanPreIdentityStates = [];
const cleanPreIdentityClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: fakeFactory,
  onStateChange: ({ current }) => cleanPreIdentityStates.push(current.status),
});
const cleanPreIdentityConnecting = cleanPreIdentityClient.connect();
await nextTurn();
const cleanPreIdentitySocket = sockets.at(-1);
cleanPreIdentitySocket.open();
cleanPreIdentitySocket.close(1000, "server closed");
await assert.rejects(cleanPreIdentityConnecting, (error) => {
  assert(error instanceof ShunterTransportError);
  assert.equal(error.kind, "transport");
  assert.equal(error.code, "1000");
  assert.deepEqual(error.details, { reason: "server closed", wasClean: true });
  return true;
});
assert.equal(cleanPreIdentityClient.state.status, "failed");
assert.deepEqual(cleanPreIdentityStates, ["connecting", "failed"]);

const errorPreIdentityClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: fakeFactory,
});
const errorPreIdentityConnecting = errorPreIdentityClient.connect();
await nextTurn();
const errorPreIdentitySocket = sockets.at(-1);
errorPreIdentitySocket.open();
errorPreIdentitySocket.dispatch("close", {
  code: 1006,
  reason: "abnormal",
  wasClean: false,
});
await assert.rejects(errorPreIdentityConnecting, (error) => {
  assert(error instanceof ShunterTransportError);
  assert.equal(error.kind, "transport");
  assert.equal(error.code, "1006");
  assert.deepEqual(error.details, { reason: "abnormal", wasClean: false });
  return true;
});
assert.equal(errorPreIdentityClient.state.status, "failed");

const closeDuringConnectSockets = [];
const closeDuringConnectStates = [];
const closeDuringConnectClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    closeDuringConnectSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => closeDuringConnectStates.push(current.status),
});
const closeDuringConnect = closeDuringConnectClient.connect();
await nextTurn();
const closeDuringConnectResult = closeDuringConnectClient.close(4000, "caller canceled");
await assert.rejects(closeDuringConnect, ShunterClosedClientError);
await closeDuringConnectResult;
assert.equal(closeDuringConnectClient.state.status, "closed");
assert.deepEqual(closeDuringConnectSockets[0].closeCalls, [{ code: 4000, reason: "caller canceled" }]);
assert.deepEqual(closeDuringConnectStates, ["connecting", "closing", "closed"]);

const disposeDuringConnectSockets = [];
const disposeDuringConnectStates = [];
const disposeDuringConnectClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    disposeDuringConnectSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => disposeDuringConnectStates.push(current.status),
});
const disposeDuringConnect = disposeDuringConnectClient.connect();
await nextTurn();
await disposeDuringConnectClient.dispose();
await assert.rejects(disposeDuringConnect, ShunterClosedClientError);
assert.equal(disposeDuringConnectClient.state.status, "closed");
assert.deepEqual(disposeDuringConnectSockets[0].closeCalls, [{ code: 1000, reason: "disposed" }]);
assert.deepEqual(disposeDuringConnectStates, ["connecting", "closing", "closed"]);
await assert.rejects(disposeDuringConnectClient.connect(), ShunterClosedClientError);

const pendingCloseSockets = [];
const pendingCloseFactory = (url, protocols) => {
  const socket = new FakeWebSocket(url, protocols);
  pendingCloseSockets.push(socket);
  return socket;
};
const pendingCloseClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: pendingCloseFactory,
});
const pendingCloseConnecting = pendingCloseClient.connect();
await nextTurn();
pendingCloseSockets[0].open();
pendingCloseSockets[0].message(identityTokenFrame().buffer);
await pendingCloseConnecting;
const pendingReducerClose = pendingCloseClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
const pendingQueryClose = pendingCloseClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x01, 0x02]),
});
const pendingTableClose = pendingCloseClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
});
assert.equal(pendingCloseSockets[0].sent.length, 3);
const pendingClientClose = pendingCloseClient.close();
await assert.rejects(pendingReducerClose, ShunterClosedClientError);
await assert.rejects(pendingQueryClose, ShunterClosedClientError);
await assert.rejects(pendingTableClose, ShunterClosedClientError);
await pendingClientClose;
assert.equal(pendingCloseClient.state.status, "closed");
assert.equal(pendingCloseSockets[0].closeCalls.length, 1);

const activeCloseSockets = [];
const activeCloseFactory = (url, protocols) => {
  const socket = new FakeWebSocket(url, protocols);
  activeCloseSockets.push(socket);
  return socket;
};
const activeCloseClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: activeCloseFactory,
});
const activeCloseConnecting = activeCloseClient.connect();
await nextTurn();
activeCloseSockets[0].open();
activeCloseSockets[0].message(identityTokenFrame().buffer);
await activeCloseConnecting;
const activeCloseHandleSubscription = activeCloseClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
});
activeCloseSockets[0].message(subscribeSingleAppliedFrame);
const activeCloseHandle = await activeCloseHandleSubscription;
assert.equal(activeCloseHandle.state.status, "active");
await activeCloseClient.close();
const activeClosed = await activeCloseHandle.closed;
assert.equal(activeClosed.reason, "error");
assert(activeClosed.error instanceof ShunterClosedClientError);
assert.deepEqual(activeCloseHandle.state, {
  status: "closed",
  error: activeClosed.error,
});

const unexpectedCloseSockets = [];
const unexpectedCloseStates = [];
const unexpectedCloseClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    unexpectedCloseSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => unexpectedCloseStates.push(current.status),
});
const unexpectedCloseConnecting = unexpectedCloseClient.connect();
await nextTurn();
unexpectedCloseSockets[0].open();
unexpectedCloseSockets[0].message(identityTokenFrame().buffer);
await unexpectedCloseConnecting;
const unexpectedCloseHandleSubscription = unexpectedCloseClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
unexpectedCloseSockets[0].message(subscribeSingleAppliedFrame);
const unexpectedCloseHandle = await unexpectedCloseHandleSubscription;
assert.deepEqual(unexpectedCloseHandle.state, { status: "active", rows: ["1-2", "3"] });
const unexpectedCloseReducer = unexpectedCloseClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
const unexpectedCloseQuery = unexpectedCloseClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x09, 0x08]),
});
assert.equal(unexpectedCloseSockets[0].sent.length, 3);
unexpectedCloseSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(unexpectedCloseClient.state.status, "closed");
const unexpectedCloseError = unexpectedCloseClient.state.error;
assert(unexpectedCloseError instanceof ShunterClosedClientError);
assert.equal(unexpectedCloseError.code, "1006");
assert.deepEqual(unexpectedCloseError.details, { reason: "lost", wasClean: false });
const assertUnexpectedCloseError = (error) => {
  assert.strictEqual(error, unexpectedCloseError);
  return true;
};
await assert.rejects(unexpectedCloseReducer, assertUnexpectedCloseError);
await assert.rejects(unexpectedCloseQuery, assertUnexpectedCloseError);
const unexpectedClosed = await unexpectedCloseHandle.closed;
assert.equal(unexpectedClosed.reason, "error");
assert.strictEqual(unexpectedClosed.error, unexpectedCloseError);
assert.deepEqual(unexpectedCloseHandle.state, {
  status: "closed",
  error: unexpectedCloseError,
});
assert.deepEqual(unexpectedCloseStates, ["connecting", "connected", "closed"]);

const transportErrorSockets = [];
const transportErrorStates = [];
const transportErrorClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    transportErrorSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => transportErrorStates.push(current.status),
});
const transportErrorConnecting = transportErrorClient.connect();
await nextTurn();
transportErrorSockets[0].open();
transportErrorSockets[0].message(identityTokenFrame().buffer);
await transportErrorConnecting;
const transportErrorHandleSubscription = transportErrorClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
transportErrorSockets[0].message(subscribeSingleAppliedFrame);
const transportErrorHandle = await transportErrorHandleSubscription;
assert.deepEqual(transportErrorHandle.state, { status: "active", rows: ["1-2", "3"] });
const transportErrorReducer = transportErrorClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
const transportErrorQuery = transportErrorClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x09, 0x08]),
});
assert.equal(transportErrorSockets[0].sent.length, 3);
transportErrorSockets[0].error();
assert.equal(transportErrorClient.state.status, "failed");
const transportError = transportErrorClient.state.error;
assert(transportError instanceof ShunterTransportError);
assert.equal(transportError.kind, "transport");
assert.equal(transportErrorSockets[0].closeCalls.length, 1);
assert.deepEqual(transportErrorSockets[0].closeCalls[0], {
  code: 1000,
  reason: "transport failure",
});
const assertTransportError = (error) => {
  assert.strictEqual(error, transportError);
  return true;
};
await assert.rejects(transportErrorReducer, assertTransportError);
await assert.rejects(transportErrorQuery, assertTransportError);
const transportErrorClosed = await transportErrorHandle.closed;
assert.equal(transportErrorClosed.reason, "error");
assert.strictEqual(transportErrorClosed.error, transportError);
assert.deepEqual(transportErrorHandle.state, {
  status: "closed",
  error: transportError,
});
assert.deepEqual(transportErrorStates, ["connecting", "connected", "failed"]);

const abortSockets = [];
const abortFactory = (url, protocols) => {
  const socket = new FakeWebSocket(url, protocols);
  abortSockets.push(socket);
  return socket;
};
const abortClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: abortFactory,
});
const abortConnecting = abortClient.connect();
await nextTurn();
abortSockets[0].open();
abortSockets[0].message(identityTokenFrame().buffer);
await abortConnecting;

const reducerAbort = new AbortController();
const abortedReducer = abortClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
  signal: reducerAbort.signal,
});
reducerAbort.abort();
await assert.rejects(abortedReducer, ShunterClosedClientError);
abortSockets[0].message(committedUpdateFrame);
assert.equal(abortClient.state.status, "connected");
const reusedReducerRequest = abortClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
abortSockets[0].message(committedUpdateFrame);
assert.deepEqual(await reusedReducerRequest, committedUpdateFrame);

const queryAbort = new AbortController();
const abortedQuery = abortClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x01, 0x02]),
  signal: queryAbort.signal,
});
queryAbort.abort();
await assert.rejects(abortedQuery, ShunterClosedClientError);
abortSockets[0].message(oneOffSuccessFrame);
assert.equal(abortClient.state.status, "connected");
const reusedQueryRequest = abortClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x01, 0x02]),
});
abortSockets[0].message(oneOffSuccessFrame);
assert.deepEqual(await reusedQueryRequest, oneOffSuccessFrame);

const subscriptionAbort = new AbortController();
const abortedSubscription = abortClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  signal: subscriptionAbort.signal,
});
subscriptionAbort.abort();
await assert.rejects(abortedSubscription, ShunterClosedClientError);
abortSockets[0].message(subscribeSingleAppliedFrame);
assert.equal(abortClient.state.status, "connected");
const reusedSubscription = abortClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
});
abortSockets[0].message(subscribeSingleAppliedFrame);
const reusedUnsubscribe = await reusedSubscription;
assert.equal(typeof reusedUnsubscribe, "function");

const viewSubscriptionAbort = new AbortController();
const abortedViewSubscription = abortClient.subscribeDeclaredView("live_users", {
  requestId: 0x41424344,
  queryId: 0x61626364,
  signal: viewSubscriptionAbort.signal,
  returnHandle: true,
});
viewSubscriptionAbort.abort();
await assert.rejects(abortedViewSubscription, ShunterClosedClientError);
abortSockets[0].message(subscribeAppliedFrame);
assert.equal(abortClient.state.status, "connected");
const reusedViewSubscription = abortClient.subscribeDeclaredView("live_users", {
  requestId: 0x41424344,
  queryId: 0x61626364,
  returnHandle: true,
});
abortSockets[0].message(subscribeAppliedFrame);
const reusedViewHandle = await reusedViewSubscription;
assert.equal(reusedViewHandle.queryId, 0x61626364);
assert.deepEqual(reusedViewHandle.state, { status: "active", rows: [] });
await abortClient.close();

const subscriptionSendFailureSockets = [];
const subscriptionSendFailureClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    subscriptionSendFailureSockets.push(socket);
    return socket;
  },
});
const subscriptionSendFailureConnecting = subscriptionSendFailureClient.connect();
await nextTurn();
subscriptionSendFailureSockets[0].open();
subscriptionSendFailureSockets[0].message(identityTokenFrame().buffer);
await subscriptionSendFailureConnecting;
const originalSubscriptionSend = subscriptionSendFailureSockets[0].send.bind(subscriptionSendFailureSockets[0]);
subscriptionSendFailureSockets[0].send = () => {
  throw new Error("subscription send denied");
};
const failedViewSend = subscriptionSendFailureClient.subscribeDeclaredView("live_users", {
  requestId: 0x41424344,
  queryId: 0x61626364,
  returnHandle: true,
});
await assert.rejects(failedViewSend, (error) => {
  assert.equal(error.kind, "transport");
  assert.match(error.message, /subscription send denied/);
  return true;
});
subscriptionSendFailureSockets[0].send = originalSubscriptionSend;
const recoveredViewSend = subscriptionSendFailureClient.subscribeDeclaredView("live_users", {
  requestId: 0x41424344,
  queryId: 0x61626364,
  returnHandle: true,
});
subscriptionSendFailureSockets[0].message(subscribeAppliedFrame);
const recoveredViewHandle = await recoveredViewSend;
assert.deepEqual(recoveredViewHandle.state, { status: "active", rows: [] });
subscriptionSendFailureSockets[0].send = () => {
  throw new Error("subscription send denied");
};
const failedTableSend = subscriptionSendFailureClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
});
await assert.rejects(failedTableSend, (error) => {
  assert.equal(error.kind, "transport");
  assert.match(error.message, /subscription send denied/);
  return true;
});
subscriptionSendFailureSockets[0].send = originalSubscriptionSend;
const recoveredTableSend = subscriptionSendFailureClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
});
subscriptionSendFailureSockets[0].message(subscribeSingleAppliedFrame);
const recoveredTableHandle = await recoveredTableSend;
assert.equal(recoveredTableHandle.state.status, "active");
assert.deepEqual(recoveredTableHandle.state.rows.map((row) => [...row]), [[1, 2], [3]]);
await subscriptionSendFailureClient.close();

const requestSendFailureSockets = [];
const requestSendFailureClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    requestSendFailureSockets.push(socket);
    return socket;
  },
});
const requestSendFailureConnecting = requestSendFailureClient.connect();
await nextTurn();
requestSendFailureSockets[0].open();
requestSendFailureSockets[0].message(identityTokenFrame().buffer);
await requestSendFailureConnecting;
const originalRequestSend = requestSendFailureSockets[0].send.bind(requestSendFailureSockets[0]);
requestSendFailureSockets[0].send = () => {
  throw new Error("request send denied");
};
const failedReducerSend = requestSendFailureClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
await assert.rejects(failedReducerSend, (error) => {
  assert.equal(error.kind, "transport");
  assert.match(error.message, /request send denied/);
  return true;
});
requestSendFailureSockets[0].send = originalRequestSend;
const recoveredReducerSend = requestSendFailureClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
requestSendFailureSockets[0].message(committedUpdateFrame);
assert.deepEqual(await recoveredReducerSend, committedUpdateFrame);
requestSendFailureSockets[0].send = () => {
  throw new Error("request send denied");
};
const failedQuerySend = requestSendFailureClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x01, 0x02]),
});
await assert.rejects(failedQuerySend, (error) => {
  assert.equal(error.kind, "transport");
  assert.match(error.message, /request send denied/);
  return true;
});
requestSendFailureSockets[0].send = originalRequestSend;
const recoveredQuerySend = requestSendFailureClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x01, 0x02]),
});
requestSendFailureSockets[0].message(oneOffSuccessFrame);
assert.deepEqual(await recoveredQuerySend, oneOffSuccessFrame);
await requestSendFailureClient.close();

const noNotifySendFailureSockets = [];
const noNotifySendFailureClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    noNotifySendFailureSockets.push(socket);
    return socket;
  },
});
const noNotifySendFailureConnecting = noNotifySendFailureClient.connect();
await nextTurn();
noNotifySendFailureSockets[0].open();
noNotifySendFailureSockets[0].message(identityTokenFrame().buffer);
await noNotifySendFailureConnecting;
const originalNoNotifySend = noNotifySendFailureSockets[0].send.bind(noNotifySendFailureSockets[0]);
noNotifySendFailureSockets[0].send = () => {
  throw new Error("no notify send denied");
};
await assert.rejects(
  noNotifySendFailureClient.callReducer("send", new Uint8Array([0xaa]), {
    requestId: 0x31323334,
    noSuccessNotify: true,
  }),
  (error) => {
    assert.equal(error.kind, "transport");
    assert.match(error.message, /no notify send denied/);
    return true;
  },
);
assert.equal(noNotifySendFailureClient.state.status, "connected");
noNotifySendFailureSockets[0].send = originalNoNotifySend;
const recoveredNoNotifyReducerSend = noNotifySendFailureClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
noNotifySendFailureSockets[0].message(committedUpdateFrame);
assert.deepEqual(await recoveredNoNotifyReducerSend, committedUpdateFrame);
await noNotifySendFailureClient.close();

const preAbortedOperationSockets = [];
const preAbortedOperationClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    preAbortedOperationSockets.push(socket);
    return socket;
  },
});
const preAbortedOperationConnecting = preAbortedOperationClient.connect();
await nextTurn();
preAbortedOperationSockets[0].open();
preAbortedOperationSockets[0].message(identityTokenFrame().buffer);
await preAbortedOperationConnecting;
const preAbortedReducerSignal = new AbortController();
preAbortedReducerSignal.abort();
await assert.rejects(
  preAbortedOperationClient.callReducer("send", new Uint8Array([0xaa]), {
    requestId: 0x21222324,
    signal: preAbortedReducerSignal.signal,
  }),
  ShunterClosedClientError,
);
const preAbortedQuerySignal = new AbortController();
preAbortedQuerySignal.abort();
await assert.rejects(
  preAbortedOperationClient.runDeclaredQuery("recent_users", {
    messageId: new Uint8Array([0x01, 0x02]),
    signal: preAbortedQuerySignal.signal,
  }),
  ShunterClosedClientError,
);
const preAbortedViewSignal = new AbortController();
preAbortedViewSignal.abort();
await assert.rejects(
  preAbortedOperationClient.subscribeDeclaredView("live_users", {
    requestId: 0x41424344,
    queryId: 0x61626364,
    signal: preAbortedViewSignal.signal,
    returnHandle: true,
  }),
  ShunterClosedClientError,
);
const preAbortedTableSignal = new AbortController();
preAbortedTableSignal.abort();
await assert.rejects(
  preAbortedOperationClient.subscribeTable("users", undefined, {
    requestId: 0x01020304,
    queryId: 0x11121314,
    signal: preAbortedTableSignal.signal,
    returnHandle: true,
  }),
  ShunterClosedClientError,
);
assert.equal(preAbortedOperationSockets[0].sent.length, 0);
assert.equal(preAbortedOperationClient.state.status, "connected");
const recoveredPreAbortedOperationReducer = preAbortedOperationClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
preAbortedOperationSockets[0].message(committedUpdateFrame);
assert.deepEqual(await recoveredPreAbortedOperationReducer, committedUpdateFrame);
await preAbortedOperationClient.close();

const reconnectSockets = [];
const reconnectFactory = (url, protocols) => {
  const socket = new FakeWebSocket(url, protocols);
  reconnectSockets.push(socket);
  return socket;
};
const reconnectStates = [];
let reconnectTokenCalls = 0;
const reconnectClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    reconnectTokenCalls += 1;
    return `token-${reconnectTokenCalls}`;
  },
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: reconnectFactory,
  onStateChange: ({ current }) => reconnectStates.push(current.status),
});
const reconnecting = reconnectClient.connect();
await nextTurn();
reconnectSockets[0].open();
reconnectSockets[0].message(identityTokenFrame({ token: "initial-token" }).buffer);
await reconnecting;
assert.equal(reconnectTokenCalls, 1);
assert.equal(reconnectSockets[0].url, "ws://127.0.0.1:3000/subscribe?token=token-1");
const reconnectHandleSubscription = reconnectClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectSockets[0].message(subscribeSingleAppliedFrame);
const reconnectHandle = await reconnectHandleSubscription;
assert.deepEqual(reconnectHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectSockets.length, 2);
assert.equal(reconnectSockets[1].url, "ws://127.0.0.1:3000/subscribe?token=token-2");
reconnectSockets[1].open();
reconnectSockets[1].message(identityTokenFrame({ token: "reconnected-token" }).buffer);
assert.equal(reconnectClient.state.status, "connected");
assert.deepEqual(
  reconnectSockets[1].sent[0],
  encodeTableSubscriptionRequest("users", {
    requestId: 1,
    queryId: 0x11121314,
  }).frame,
);
reconnectSockets[1].message(reconnectSubscribeSingleAppliedFrame);
assert.deepEqual(reconnectHandle.state, { status: "active", rows: ["4-5"] });
await nextTurn();
reconnectSockets[1].dispatch("close", { code: 1006, reason: "lost again", wasClean: false });
assert.equal(reconnectClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectSockets.length, 3);
reconnectSockets[2].open();
reconnectSockets[2].message(identityTokenFrame({ token: "second-reconnected-token" }).buffer);
assert.equal(reconnectClient.state.status, "connected");
assert.deepEqual(
  reconnectSockets[2].sent[0],
  encodeTableSubscriptionRequest("users", {
    requestId: 2,
    queryId: 0x11121314,
  }).frame,
);
reconnectSockets[2].message(secondReconnectSubscribeSingleAppliedFrame);
assert.deepEqual(reconnectHandle.state, { status: "active", rows: ["4-5"] });
assert.deepEqual(reconnectStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "connected",
]);
await reconnectClient.close();

const reconnectNoReplaySockets = [];
const reconnectNoReplayStates = [];
const reconnectNoReplayClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
    resubscribe: false,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectNoReplaySockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectNoReplayStates.push(current.status),
});
const reconnectNoReplayConnecting = reconnectNoReplayClient.connect();
await nextTurn();
reconnectNoReplaySockets[0].open();
reconnectNoReplaySockets[0].message(identityTokenFrame().buffer);
await reconnectNoReplayConnecting;
const reconnectNoReplayHandleSubscription = reconnectNoReplayClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectNoReplaySockets[0].message(subscribeSingleAppliedFrame);
const reconnectNoReplayHandle = await reconnectNoReplayHandleSubscription;
assert.deepEqual(reconnectNoReplayHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectNoReplaySockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectNoReplayClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectNoReplaySockets.length, 2);
reconnectNoReplaySockets[1].open();
reconnectNoReplaySockets[1].message(identityTokenFrame().buffer);
assert.equal(reconnectNoReplayClient.state.status, "connected");
assert.equal(reconnectNoReplaySockets[1].sent.length, 0);
assert.deepEqual(reconnectNoReplayHandle.state, { status: "active", rows: ["1-2", "3"] });
assert.deepEqual(reconnectNoReplayStates, ["connecting", "connected", "reconnecting", "connecting", "connected"]);
await reconnectNoReplayClient.close();
const reconnectNoReplayClosed = await reconnectNoReplayHandle.closed;
assert.equal(reconnectNoReplayClosed.reason, "error");
assert(reconnectNoReplayClosed.error instanceof ShunterClosedClientError);

const reconnectZeroSockets = [];
const reconnectZeroStates = [];
let reconnectZeroTokenCalls = 0;
const reconnectZeroClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    reconnectZeroTokenCalls += 1;
    return `zero-token-${reconnectZeroTokenCalls}`;
  },
  reconnect: {
    enabled: true,
    maxAttempts: 0,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectZeroSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectZeroStates.push(current.status),
});
const reconnectZeroConnecting = reconnectZeroClient.connect();
await nextTurn();
reconnectZeroSockets[0].open();
reconnectZeroSockets[0].message(identityTokenFrame().buffer);
await reconnectZeroConnecting;
assert.equal(reconnectZeroTokenCalls, 1);
const reconnectZeroHandleSubscription = reconnectZeroClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectZeroSockets[0].message(subscribeSingleAppliedFrame);
const reconnectZeroHandle = await reconnectZeroHandleSubscription;
assert.deepEqual(reconnectZeroHandle.state, { status: "active", rows: ["1-2", "3"] });
const reconnectZeroReducer = reconnectZeroClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
const reconnectZeroQuery = reconnectZeroClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x09, 0x08]),
});
reconnectZeroSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectZeroClient.state.status, "closed");
const reconnectZeroError = reconnectZeroClient.state.error;
assert(reconnectZeroError instanceof ShunterClosedClientError);
assert.equal(reconnectZeroError.code, "1006");
assert.equal(reconnectZeroTokenCalls, 1);
assert.equal(reconnectZeroSockets.length, 1);
const assertReconnectZeroError = (error) => {
  assert.strictEqual(error, reconnectZeroError);
  return true;
};
await assert.rejects(reconnectZeroReducer, assertReconnectZeroError);
await assert.rejects(reconnectZeroQuery, assertReconnectZeroError);
const reconnectZeroClosed = await reconnectZeroHandle.closed;
assert.equal(reconnectZeroClosed.reason, "error");
assert.strictEqual(reconnectZeroClosed.error, reconnectZeroError);
assert.deepEqual(reconnectZeroHandle.state, {
  status: "closed",
  error: reconnectZeroError,
});
assert.deepEqual(reconnectZeroStates, ["connecting", "connected", "closed"]);

const reconnectCloseSockets = [];
const reconnectCloseStates = [];
const reconnectCloseClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectCloseSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectCloseStates.push(current.status),
});
const reconnectCloseConnecting = reconnectCloseClient.connect();
await nextTurn();
reconnectCloseSockets[0].open();
reconnectCloseSockets[0].message(identityTokenFrame().buffer);
await reconnectCloseConnecting;
const reconnectCloseHandleSubscription = reconnectCloseClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectCloseSockets[0].message(subscribeSingleAppliedFrame);
const reconnectCloseHandle = await reconnectCloseHandleSubscription;
assert.deepEqual(reconnectCloseHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectCloseSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectCloseClient.state.status, "reconnecting");
await reconnectCloseClient.close();
await nextTurn();
assert.equal(reconnectCloseClient.state.status, "closed");
assert.equal(reconnectCloseSockets.length, 1);
const reconnectCloseClosed = await reconnectCloseHandle.closed;
assert.equal(reconnectCloseClosed.reason, "error");
assert(reconnectCloseClosed.error instanceof ShunterClosedClientError);
assert.deepEqual(reconnectCloseHandle.state, {
  status: "closed",
  error: reconnectCloseClosed.error,
});
assert.deepEqual(reconnectCloseStates, ["connecting", "connected", "reconnecting", "closing", "closed"]);

const reconnectPendingTokenCloseSockets = [];
const reconnectPendingTokenCloseStates = [];
let reconnectPendingTokenCloseCalls = 0;
let resolveReconnectPendingTokenClose;
const reconnectPendingTokenCloseClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    reconnectPendingTokenCloseCalls += 1;
    if (reconnectPendingTokenCloseCalls === 1) {
      return "initial-token";
    }
    return new Promise((resolve) => {
      resolveReconnectPendingTokenClose = resolve;
    });
  },
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectPendingTokenCloseSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectPendingTokenCloseStates.push(current.status),
});
const reconnectPendingTokenCloseConnecting = reconnectPendingTokenCloseClient.connect();
await nextTurn();
assert.equal(reconnectPendingTokenCloseSockets[0].url, "ws://127.0.0.1:3000/subscribe?token=initial-token");
reconnectPendingTokenCloseSockets[0].open();
reconnectPendingTokenCloseSockets[0].message(identityTokenFrame().buffer);
await reconnectPendingTokenCloseConnecting;
const reconnectPendingTokenCloseHandleSubscription = reconnectPendingTokenCloseClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectPendingTokenCloseSockets[0].message(subscribeSingleAppliedFrame);
const reconnectPendingTokenCloseHandle = await reconnectPendingTokenCloseHandleSubscription;
assert.deepEqual(reconnectPendingTokenCloseHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectPendingTokenCloseSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectPendingTokenCloseClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectPendingTokenCloseCalls, 2);
assert.equal(reconnectPendingTokenCloseClient.state.status, "connecting");
assert.equal(reconnectPendingTokenCloseSockets.length, 1);
const observedReconnectPendingTokenClose = reconnectPendingTokenCloseClient.connect();
const reconnectPendingTokenCloseClosed = reconnectPendingTokenCloseClient.close(4003, "caller stopped before token");
await assert.rejects(observedReconnectPendingTokenClose, ShunterClosedClientError);
await reconnectPendingTokenCloseClosed;
assert.equal(reconnectPendingTokenCloseClient.state.status, "closed");
const reconnectPendingTokenClosed = await reconnectPendingTokenCloseHandle.closed;
assert.equal(reconnectPendingTokenClosed.reason, "error");
assert(reconnectPendingTokenClosed.error instanceof ShunterClosedClientError);
assert.deepEqual(reconnectPendingTokenCloseHandle.state, {
  status: "closed",
  error: reconnectPendingTokenClosed.error,
});
resolveReconnectPendingTokenClose("too-late");
await nextTurn();
assert.equal(reconnectPendingTokenCloseSockets.length, 1);
assert.equal(reconnectPendingTokenCloseClient.state.status, "closed");
assert.deepEqual(reconnectPendingTokenCloseStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "closing",
  "closed",
]);

const reconnectPendingTokenDisposeSockets = [];
const reconnectPendingTokenDisposeStates = [];
let reconnectPendingTokenDisposeCalls = 0;
let resolveReconnectPendingTokenDispose;
const reconnectPendingTokenDisposeClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    reconnectPendingTokenDisposeCalls += 1;
    if (reconnectPendingTokenDisposeCalls === 1) {
      return "initial-token";
    }
    return new Promise((resolve) => {
      resolveReconnectPendingTokenDispose = resolve;
    });
  },
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectPendingTokenDisposeSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectPendingTokenDisposeStates.push(current.status),
});
const reconnectPendingTokenDisposeConnecting = reconnectPendingTokenDisposeClient.connect();
await nextTurn();
assert.equal(reconnectPendingTokenDisposeSockets[0].url, "ws://127.0.0.1:3000/subscribe?token=initial-token");
reconnectPendingTokenDisposeSockets[0].open();
reconnectPendingTokenDisposeSockets[0].message(identityTokenFrame().buffer);
await reconnectPendingTokenDisposeConnecting;
const reconnectPendingTokenDisposeHandleSubscription = reconnectPendingTokenDisposeClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectPendingTokenDisposeSockets[0].message(subscribeSingleAppliedFrame);
const reconnectPendingTokenDisposeHandle = await reconnectPendingTokenDisposeHandleSubscription;
assert.deepEqual(reconnectPendingTokenDisposeHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectPendingTokenDisposeSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectPendingTokenDisposeClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectPendingTokenDisposeCalls, 2);
assert.equal(reconnectPendingTokenDisposeClient.state.status, "connecting");
assert.equal(reconnectPendingTokenDisposeSockets.length, 1);
const observedReconnectPendingTokenDispose = reconnectPendingTokenDisposeClient.connect();
const reconnectPendingTokenDisposed = reconnectPendingTokenDisposeClient.dispose();
await assert.rejects(observedReconnectPendingTokenDispose, ShunterClosedClientError);
await reconnectPendingTokenDisposed;
assert.equal(reconnectPendingTokenDisposeClient.state.status, "closed");
const reconnectPendingTokenDisposedHandleClosed = await reconnectPendingTokenDisposeHandle.closed;
assert.equal(reconnectPendingTokenDisposedHandleClosed.reason, "error");
assert(reconnectPendingTokenDisposedHandleClosed.error instanceof ShunterClosedClientError);
assert.deepEqual(reconnectPendingTokenDisposeHandle.state, {
  status: "closed",
  error: reconnectPendingTokenDisposedHandleClosed.error,
});
resolveReconnectPendingTokenDispose("too-late");
await nextTurn();
assert.equal(reconnectPendingTokenDisposeSockets.length, 1);
assert.equal(reconnectPendingTokenDisposeClient.state.status, "closed");
await assert.rejects(reconnectPendingTokenDisposeClient.connect(), ShunterClosedClientError);
assert.deepEqual(reconnectPendingTokenDisposeStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "closing",
  "closed",
]);

for (const shutdownMode of ["close", "dispose"]) {
  const reconnectRejectedTokenShutdownSockets = [];
  const reconnectRejectedTokenShutdownStates = [];
  let reconnectRejectedTokenShutdownCalls = 0;
  let rejectReconnectRefreshToken;
  const reconnectRejectedTokenShutdownClient = createShunterClient({
    url: "ws://127.0.0.1:3000/subscribe",
    protocol: shunterProtocol,
    token: () => {
      reconnectRejectedTokenShutdownCalls += 1;
      if (reconnectRejectedTokenShutdownCalls === 1) {
        return "initial-token";
      }
      return new Promise((_, reject) => {
        rejectReconnectRefreshToken = reject;
      });
    },
    reconnect: {
      enabled: true,
      maxAttempts: 1,
      initialDelayMs: 0,
      maxDelayMs: 0,
    },
    webSocketFactory: (url, protocols) => {
      const socket = new FakeWebSocket(url, protocols);
      reconnectRejectedTokenShutdownSockets.push(socket);
      return socket;
    },
    onStateChange: ({ current }) => reconnectRejectedTokenShutdownStates.push(current.status),
  });
  const reconnectRejectedTokenShutdownConnecting = reconnectRejectedTokenShutdownClient.connect();
  await nextTurn();
  assert.equal(reconnectRejectedTokenShutdownSockets[0].url, "ws://127.0.0.1:3000/subscribe?token=initial-token");
  reconnectRejectedTokenShutdownSockets[0].open();
  reconnectRejectedTokenShutdownSockets[0].message(identityTokenFrame().buffer);
  await reconnectRejectedTokenShutdownConnecting;
  const reconnectRejectedTokenShutdownHandleSubscription = reconnectRejectedTokenShutdownClient.subscribeTable(
    "users",
    undefined,
    {
      requestId: 0x01020304,
      queryId: 0x11121314,
      returnHandle: true,
      decodeRow: (row) => [...row].join("-"),
    },
  );
  reconnectRejectedTokenShutdownSockets[0].message(subscribeSingleAppliedFrame);
  const reconnectRejectedTokenShutdownHandle = await reconnectRejectedTokenShutdownHandleSubscription;
  assert.deepEqual(reconnectRejectedTokenShutdownHandle.state, { status: "active", rows: ["1-2", "3"] });
  reconnectRejectedTokenShutdownSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
  assert.equal(reconnectRejectedTokenShutdownClient.state.status, "reconnecting");
  await nextTurn();
  assert.equal(reconnectRejectedTokenShutdownCalls, 2);
  assert.equal(reconnectRejectedTokenShutdownClient.state.status, "connecting");
  assert.equal(reconnectRejectedTokenShutdownSockets.length, 1);
  const observedReconnectRejectedTokenShutdown = reconnectRejectedTokenShutdownClient.connect();
  const reconnectRejectedTokenShutdownClosed = shutdownMode === "dispose"
    ? reconnectRejectedTokenShutdownClient.dispose()
    : reconnectRejectedTokenShutdownClient.close(4004, "caller stopped before token rejection");
  await assert.rejects(observedReconnectRejectedTokenShutdown, ShunterClosedClientError);
  await reconnectRejectedTokenShutdownClosed;
  const reconnectRejectedTokenShutdownHandleClosed = await reconnectRejectedTokenShutdownHandle.closed;
  assert.equal(reconnectRejectedTokenShutdownHandleClosed.reason, "error");
  assert(reconnectRejectedTokenShutdownHandleClosed.error instanceof ShunterClosedClientError);
  assert.deepEqual(reconnectRejectedTokenShutdownHandle.state, {
    status: "closed",
    error: reconnectRejectedTokenShutdownHandleClosed.error,
  });
  rejectReconnectRefreshToken(new Error("too-late refresh"));
  await nextTurn();
  assert.equal(reconnectRejectedTokenShutdownSockets.length, 1);
  assert.equal(reconnectRejectedTokenShutdownClient.state.status, "closed");
  if (shutdownMode === "dispose") {
    await assert.rejects(reconnectRejectedTokenShutdownClient.connect(), ShunterClosedClientError);
  }
  assert.deepEqual(reconnectRejectedTokenShutdownStates, [
    "connecting",
    "connected",
    "reconnecting",
    "connecting",
    "closing",
    "closed",
  ]);
}

const reconnectHandshakeCloseSockets = [];
const reconnectHandshakeCloseStates = [];
const reconnectHandshakeCloseClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectHandshakeCloseSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectHandshakeCloseStates.push(current.status),
});
const reconnectHandshakeCloseConnecting = reconnectHandshakeCloseClient.connect();
await nextTurn();
reconnectHandshakeCloseSockets[0].open();
reconnectHandshakeCloseSockets[0].message(identityTokenFrame().buffer);
await reconnectHandshakeCloseConnecting;
const reconnectHandshakeCloseHandleSubscription = reconnectHandshakeCloseClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectHandshakeCloseSockets[0].message(subscribeSingleAppliedFrame);
const reconnectHandshakeCloseHandle = await reconnectHandshakeCloseHandleSubscription;
assert.deepEqual(reconnectHandshakeCloseHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectHandshakeCloseSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
await nextTurn();
assert.equal(reconnectHandshakeCloseClient.state.status, "connecting");
assert.equal(reconnectHandshakeCloseSockets.length, 2);
const observedHandshakeReconnect = reconnectHandshakeCloseClient.connect();
const reconnectHandshakeClose = reconnectHandshakeCloseClient.close(4001, "caller stopped");
await assert.rejects(observedHandshakeReconnect, ShunterClosedClientError);
await reconnectHandshakeClose;
assert.equal(reconnectHandshakeCloseClient.state.status, "closed");
assert.deepEqual(reconnectHandshakeCloseSockets[1].closeCalls, [{ code: 4001, reason: "caller stopped" }]);
const reconnectHandshakeClosed = await reconnectHandshakeCloseHandle.closed;
assert.equal(reconnectHandshakeClosed.reason, "error");
assert(reconnectHandshakeClosed.error instanceof ShunterClosedClientError);
assert.deepEqual(reconnectHandshakeCloseHandle.state, {
  status: "closed",
  error: reconnectHandshakeClosed.error,
});
assert.deepEqual(reconnectHandshakeCloseStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "closing",
  "closed",
]);

const reconnectUnsubscribeSockets = [];
const reconnectUnsubscribeStates = [];
const reconnectUnsubscribeClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectUnsubscribeSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectUnsubscribeStates.push(current.status),
});
const reconnectUnsubscribeConnecting = reconnectUnsubscribeClient.connect();
await nextTurn();
reconnectUnsubscribeSockets[0].open();
reconnectUnsubscribeSockets[0].message(identityTokenFrame().buffer);
await reconnectUnsubscribeConnecting;
const reconnectUnsubscribeHandleSubscription = reconnectUnsubscribeClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectUnsubscribeSockets[0].message(subscribeSingleAppliedFrame);
const reconnectUnsubscribeHandle = await reconnectUnsubscribeHandleSubscription;
assert.deepEqual(reconnectUnsubscribeHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectUnsubscribeSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectUnsubscribeClient.state.status, "reconnecting");
await reconnectUnsubscribeHandle.unsubscribe();
assert.deepEqual(await reconnectUnsubscribeHandle.closed, { reason: "unsubscribed" });
assert.deepEqual(reconnectUnsubscribeHandle.state, { status: "closed" });
await nextTurn();
assert.equal(reconnectUnsubscribeSockets.length, 2);
reconnectUnsubscribeSockets[1].open();
reconnectUnsubscribeSockets[1].message(identityTokenFrame().buffer);
assert.equal(reconnectUnsubscribeClient.state.status, "connected");
assert.equal(reconnectUnsubscribeSockets[1].sent.length, 0);
assert.deepEqual(reconnectUnsubscribeStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "connected",
]);
await reconnectUnsubscribeClient.close();

const reconnectViewUnsubscribeSockets = [];
const reconnectViewUnsubscribeStates = [];
const reconnectViewUnsubscribeClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectViewUnsubscribeSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectViewUnsubscribeStates.push(current.status),
});
const reconnectViewUnsubscribeConnecting = reconnectViewUnsubscribeClient.connect();
await nextTurn();
reconnectViewUnsubscribeSockets[0].open();
reconnectViewUnsubscribeSockets[0].message(identityTokenFrame().buffer);
await reconnectViewUnsubscribeConnecting;
const reconnectViewUnsubscribeHandleSubscription = reconnectViewUnsubscribeClient.subscribeDeclaredView("live_users", {
  requestId: 0x41424344,
  queryId: 0x61626364,
  returnHandle: true,
});
reconnectViewUnsubscribeSockets[0].message(subscribeAppliedFrame);
const reconnectViewUnsubscribeHandle = await reconnectViewUnsubscribeHandleSubscription;
assert.deepEqual(reconnectViewUnsubscribeHandle.state, { status: "active", rows: [] });
reconnectViewUnsubscribeSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectViewUnsubscribeClient.state.status, "reconnecting");
await reconnectViewUnsubscribeHandle.unsubscribe();
assert.deepEqual(await reconnectViewUnsubscribeHandle.closed, { reason: "unsubscribed" });
assert.deepEqual(reconnectViewUnsubscribeHandle.state, { status: "closed" });
await nextTurn();
assert.equal(reconnectViewUnsubscribeSockets.length, 2);
reconnectViewUnsubscribeSockets[1].open();
reconnectViewUnsubscribeSockets[1].message(identityTokenFrame().buffer);
assert.equal(reconnectViewUnsubscribeClient.state.status, "connected");
assert.equal(reconnectViewUnsubscribeSockets[1].sent.length, 0);
assert.deepEqual(reconnectViewUnsubscribeStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "connected",
]);
await reconnectViewUnsubscribeClient.close();

const reconnectPendingUnsubscribeSockets = [];
const reconnectPendingUnsubscribeStates = [];
const reconnectPendingUnsubscribeClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectPendingUnsubscribeSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectPendingUnsubscribeStates.push(current.status),
});
const reconnectPendingUnsubscribeConnecting = reconnectPendingUnsubscribeClient.connect();
await nextTurn();
reconnectPendingUnsubscribeSockets[0].open();
reconnectPendingUnsubscribeSockets[0].message(identityTokenFrame().buffer);
await reconnectPendingUnsubscribeConnecting;
const reconnectPendingUnsubscribeHandleSubscription = reconnectPendingUnsubscribeClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectPendingUnsubscribeSockets[0].message(subscribeSingleAppliedFrame);
const reconnectPendingUnsubscribeHandle = await reconnectPendingUnsubscribeHandleSubscription;
assert.deepEqual(reconnectPendingUnsubscribeHandle.state, { status: "active", rows: ["1-2", "3"] });
const reconnectPendingUnsubscribe = reconnectPendingUnsubscribeHandle.unsubscribe();
assert.equal(reconnectPendingUnsubscribeSockets[0].sent.length, 2);
reconnectPendingUnsubscribeSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectPendingUnsubscribeClient.state.status, "reconnecting");
await reconnectPendingUnsubscribe;
const reconnectPendingUnsubscribed = await reconnectPendingUnsubscribeHandle.closed;
assert.equal(reconnectPendingUnsubscribed.reason, "error");
assert(reconnectPendingUnsubscribed.error instanceof ShunterClosedClientError);
assert.deepEqual(reconnectPendingUnsubscribeHandle.state, {
  status: "closed",
  error: reconnectPendingUnsubscribed.error,
});
await nextTurn();
assert.equal(reconnectPendingUnsubscribeSockets.length, 2);
reconnectPendingUnsubscribeSockets[1].open();
reconnectPendingUnsubscribeSockets[1].message(identityTokenFrame().buffer);
assert.equal(reconnectPendingUnsubscribeClient.state.status, "connected");
assert.equal(reconnectPendingUnsubscribeSockets[1].sent.length, 0);
assert.deepEqual(reconnectPendingUnsubscribeStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "connected",
]);
await reconnectPendingUnsubscribeClient.close();

const reconnectPendingViewUnsubscribeSockets = [];
const reconnectPendingViewUnsubscribeStates = [];
const reconnectPendingViewUnsubscribeClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectPendingViewUnsubscribeSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectPendingViewUnsubscribeStates.push(current.status),
});
const reconnectPendingViewUnsubscribeConnecting = reconnectPendingViewUnsubscribeClient.connect();
await nextTurn();
reconnectPendingViewUnsubscribeSockets[0].open();
reconnectPendingViewUnsubscribeSockets[0].message(identityTokenFrame().buffer);
await reconnectPendingViewUnsubscribeConnecting;
const reconnectPendingViewUnsubscribeHandleSubscription =
  reconnectPendingViewUnsubscribeClient.subscribeDeclaredView("live_users", {
    requestId: 0x41424344,
    queryId: 0x61626364,
    returnHandle: true,
  });
reconnectPendingViewUnsubscribeSockets[0].message(subscribeAppliedFrame);
const reconnectPendingViewUnsubscribeHandle = await reconnectPendingViewUnsubscribeHandleSubscription;
assert.deepEqual(reconnectPendingViewUnsubscribeHandle.state, { status: "active", rows: [] });
const reconnectPendingViewUnsubscribe = reconnectPendingViewUnsubscribeHandle.unsubscribe();
assert.equal(reconnectPendingViewUnsubscribeSockets[0].sent.length, 2);
reconnectPendingViewUnsubscribeSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectPendingViewUnsubscribeClient.state.status, "reconnecting");
await reconnectPendingViewUnsubscribe;
const reconnectPendingViewUnsubscribed = await reconnectPendingViewUnsubscribeHandle.closed;
assert.equal(reconnectPendingViewUnsubscribed.reason, "error");
assert(reconnectPendingViewUnsubscribed.error instanceof ShunterClosedClientError);
assert.deepEqual(reconnectPendingViewUnsubscribeHandle.state, {
  status: "closed",
  error: reconnectPendingViewUnsubscribed.error,
});
await nextTurn();
assert.equal(reconnectPendingViewUnsubscribeSockets.length, 2);
reconnectPendingViewUnsubscribeSockets[1].open();
reconnectPendingViewUnsubscribeSockets[1].message(identityTokenFrame().buffer);
assert.equal(reconnectPendingViewUnsubscribeClient.state.status, "connected");
assert.equal(reconnectPendingViewUnsubscribeSockets[1].sent.length, 0);
assert.deepEqual(reconnectPendingViewUnsubscribeStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "connected",
]);
await reconnectPendingViewUnsubscribeClient.close();

const exhaustionSockets = [];
const exhaustionFactory = (url, protocols) => {
  const socket = new FakeWebSocket(url, protocols);
  exhaustionSockets.push(socket);
  return socket;
};
const exhaustionStates = [];
let exhaustionTokenCalls = 0;
const exhaustionClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    exhaustionTokenCalls += 1;
    return `token-${exhaustionTokenCalls}`;
  },
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: exhaustionFactory,
  onStateChange: ({ current }) => exhaustionStates.push(current.status),
});
const exhaustionConnecting = exhaustionClient.connect();
await nextTurn();
exhaustionSockets[0].open();
exhaustionSockets[0].message(identityTokenFrame({ token: "initial-token" }).buffer);
await exhaustionConnecting;
assert.equal(exhaustionTokenCalls, 1);
assert.equal(exhaustionSockets[0].url, "ws://127.0.0.1:3000/subscribe?token=token-1");
const exhaustionHandleSubscription = exhaustionClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
exhaustionSockets[0].message(subscribeSingleAppliedFrame);
const exhaustionHandle = await exhaustionHandleSubscription;
assert.deepEqual(exhaustionHandle.state, { status: "active", rows: ["1-2", "3"] });
const exhaustionReducer = exhaustionClient.callReducer("send", new Uint8Array([0xaa]), {
  requestId: 0x21222324,
});
const exhaustionQuery = exhaustionClient.runDeclaredQuery("recent_users", {
  messageId: new Uint8Array([0x09, 0x08]),
});
assert.equal(exhaustionSockets[0].sent.length, 3);
exhaustionSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(exhaustionClient.state.status, "reconnecting");
const assertInitialDisconnect = (error) => {
  assert(error instanceof ShunterClosedClientError);
  assert.equal(error.kind, "closed");
  assert.equal(error.code, "1006");
  assert.deepEqual(error.details, { reason: "lost", wasClean: false });
  return true;
};
await assert.rejects(exhaustionReducer, assertInitialDisconnect);
await assert.rejects(exhaustionQuery, assertInitialDisconnect);
assert.deepEqual(exhaustionHandle.state, { status: "active", rows: ["1-2", "3"] });
await nextTurn();
assert.equal(exhaustionTokenCalls, 2);
assert.equal(exhaustionSockets.length, 2);
assert.equal(exhaustionSockets[1].url, "ws://127.0.0.1:3000/subscribe?token=token-2");
const observedReconnect = exhaustionClient.connect();
exhaustionSockets[1].open();
exhaustionSockets[1].dispatch("close", {
  code: 1006,
  reason: "identity missing",
  wasClean: false,
});
const exhaustionError = await rejectByNextTurn(observedReconnect, (error) => {
  assert(error instanceof ShunterTransportError);
  assert.equal(error.kind, "transport");
  assert.equal(error.code, "1006");
  assert.deepEqual(error.details, { reason: "identity missing", wasClean: false });
});
assert.equal(exhaustionClient.state.status, "closed");
assert.strictEqual(exhaustionClient.state.error, exhaustionError);
const exhaustionClosed = await exhaustionHandle.closed;
assert.equal(exhaustionClosed.reason, "error");
assert.strictEqual(exhaustionClosed.error, exhaustionError);
assert.deepEqual(exhaustionHandle.state, {
  status: "closed",
  error: exhaustionError,
});
assert.deepEqual(exhaustionStates, ["connecting", "connected", "reconnecting", "connecting", "closed"]);

const reconnectProtocolSockets = [];
const reconnectProtocolFactory = (url, protocols) => {
  const socket = new FakeWebSocket(url, protocols);
  reconnectProtocolSockets.push(socket);
  return socket;
};
const reconnectProtocolStates = [];
const reconnectProtocolClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: reconnectProtocolFactory,
  onStateChange: ({ current }) => reconnectProtocolStates.push(current.status),
});
const reconnectProtocolConnecting = reconnectProtocolClient.connect();
await nextTurn();
reconnectProtocolSockets[0].open();
reconnectProtocolSockets[0].message(identityTokenFrame().buffer);
await reconnectProtocolConnecting;
const reconnectProtocolHandleSubscription = reconnectProtocolClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectProtocolSockets[0].message(subscribeSingleAppliedFrame);
const reconnectProtocolHandle = await reconnectProtocolHandleSubscription;
assert.deepEqual(reconnectProtocolHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectProtocolSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectProtocolClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectProtocolSockets.length, 2);
const observedProtocolReconnect = reconnectProtocolClient.connect();
reconnectProtocolSockets[1].open("v1.bsatn.spacetimedb");
const reconnectProtocolError = await rejectByNextTurn(observedProtocolReconnect, (error) => {
  assert(error instanceof ShunterProtocolMismatchError);
  assert.equal(error.kind, "protocol_mismatch");
  assert.equal(error.code, "unsupported_selected_subprotocol");
  assert.equal(error.receivedSubprotocol, "v1.bsatn.spacetimedb");
});
assert.equal(reconnectProtocolClient.state.status, "closed");
assert.strictEqual(reconnectProtocolClient.state.error, reconnectProtocolError);
assert.equal(reconnectProtocolSockets[1].closeCalls.length, 1);
const reconnectProtocolClosed = await reconnectProtocolHandle.closed;
assert.equal(reconnectProtocolClosed.reason, "error");
assert.strictEqual(reconnectProtocolClosed.error, reconnectProtocolError);
assert.deepEqual(reconnectProtocolHandle.state, {
  status: "closed",
  error: reconnectProtocolError,
});
assert.deepEqual(reconnectProtocolStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "failed",
  "closed",
]);

const reconnectMissingProtocolSockets = [];
const reconnectMissingProtocolStates = [];
const reconnectMissingProtocolClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectMissingProtocolSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectMissingProtocolStates.push(current.status),
});
const reconnectMissingProtocolConnecting = reconnectMissingProtocolClient.connect();
await nextTurn();
reconnectMissingProtocolSockets[0].open();
reconnectMissingProtocolSockets[0].message(identityTokenFrame().buffer);
await reconnectMissingProtocolConnecting;
const reconnectMissingProtocolHandleSubscription = reconnectMissingProtocolClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectMissingProtocolSockets[0].message(subscribeSingleAppliedFrame);
const reconnectMissingProtocolHandle = await reconnectMissingProtocolHandleSubscription;
assert.deepEqual(reconnectMissingProtocolHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectMissingProtocolSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectMissingProtocolClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectMissingProtocolSockets.length, 2);
const observedMissingProtocolReconnect = reconnectMissingProtocolClient.connect();
reconnectMissingProtocolSockets[1].open("");
const reconnectMissingProtocolError = await rejectByNextTurn(observedMissingProtocolReconnect, (error) => {
  assert(error instanceof ShunterProtocolMismatchError);
  assert.equal(error.kind, "protocol_mismatch");
  assert.equal(error.code, "unsupported_selected_subprotocol");
  assert.equal(error.receivedSubprotocol, "");
});
assert.equal(reconnectMissingProtocolClient.state.status, "closed");
assert.strictEqual(reconnectMissingProtocolClient.state.error, reconnectMissingProtocolError);
assert.equal(reconnectMissingProtocolSockets[1].closeCalls.length, 1);
const reconnectMissingProtocolClosed = await reconnectMissingProtocolHandle.closed;
assert.equal(reconnectMissingProtocolClosed.reason, "error");
assert.strictEqual(reconnectMissingProtocolClosed.error, reconnectMissingProtocolError);
assert.deepEqual(reconnectMissingProtocolHandle.state, {
  status: "closed",
  error: reconnectMissingProtocolError,
});
assert.deepEqual(reconnectMissingProtocolStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "failed",
  "closed",
]);

const reconnectReplayFailureSockets = [];
const reconnectReplayFailureStates = [];
const reconnectReplayFailureClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    if (reconnectReplayFailureSockets.length === 1) {
      socket.send = () => {
        throw new Error("resubscribe send denied");
      };
    }
    reconnectReplayFailureSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectReplayFailureStates.push(current.status),
});
const reconnectReplayFailureConnecting = reconnectReplayFailureClient.connect();
await nextTurn();
reconnectReplayFailureSockets[0].open();
reconnectReplayFailureSockets[0].message(identityTokenFrame().buffer);
await reconnectReplayFailureConnecting;
const reconnectReplayFailureHandleSubscription = reconnectReplayFailureClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectReplayFailureSockets[0].message(subscribeSingleAppliedFrame);
const reconnectReplayFailureHandle = await reconnectReplayFailureHandleSubscription;
assert.deepEqual(reconnectReplayFailureHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectReplayFailureSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectReplayFailureClient.state.status, "reconnecting");
await nextTurn();
assert.equal(reconnectReplayFailureSockets.length, 2);
const observedReplayFailureReconnect = reconnectReplayFailureClient.connect();
reconnectReplayFailureSockets[1].open();
reconnectReplayFailureSockets[1].message(identityTokenFrame().buffer);
const reconnectReplayFailureError = await rejectByNextTurn(observedReplayFailureReconnect, (error) => {
  assert.equal(error.kind, "transport");
  assert.match(error.message, /resubscribe send denied/);
});
assert.equal(reconnectReplayFailureClient.state.status, "closed");
assert.strictEqual(reconnectReplayFailureClient.state.error, reconnectReplayFailureError);
assert.deepEqual(reconnectReplayFailureSockets[1].closeCalls, [{ code: 1000, reason: "protocol failure" }]);
const reconnectReplayFailureClosed = await reconnectReplayFailureHandle.closed;
assert.equal(reconnectReplayFailureClosed.reason, "error");
assert.strictEqual(reconnectReplayFailureClosed.error, reconnectReplayFailureError);
assert.deepEqual(reconnectReplayFailureHandle.state, {
  status: "closed",
  error: reconnectReplayFailureError,
});
assert.deepEqual(reconnectReplayFailureStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "connected",
  "failed",
  "closed",
]);

const reconnectReplayErrorSockets = [];
const reconnectReplayErrorStates = [];
const reconnectReplayErrorClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectReplayErrorSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectReplayErrorStates.push(current.status),
});
const reconnectReplayErrorConnecting = reconnectReplayErrorClient.connect();
await nextTurn();
reconnectReplayErrorSockets[0].open();
reconnectReplayErrorSockets[0].message(identityTokenFrame().buffer);
await reconnectReplayErrorConnecting;
const reconnectReplayErrorHandleSubscription = reconnectReplayErrorClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectReplayErrorSockets[0].message(subscribeSingleAppliedFrame);
const reconnectReplayErrorHandle = await reconnectReplayErrorHandleSubscription;
assert.deepEqual(reconnectReplayErrorHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectReplayErrorSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectReplayErrorClient.state.status, "reconnecting");
await nextTurn();
const observedReplayErrorReconnect = reconnectReplayErrorClient.connect();
reconnectReplayErrorSockets[1].open();
reconnectReplayErrorSockets[1].message(identityTokenFrame().buffer);
await observedReplayErrorReconnect;
assert.equal(reconnectReplayErrorClient.state.status, "connected");
assert.deepEqual(
  reconnectReplayErrorSockets[1].sent[0],
  encodeTableSubscriptionRequest("users", {
    requestId: 1,
    queryId: 0x11121314,
  }).frame,
);
reconnectReplayErrorSockets[1].message(reconnectSubscriptionErrorFrame);
assert.equal(reconnectReplayErrorClient.state.status, "connected");
const reconnectReplayErrorClosed = await reconnectReplayErrorHandle.closed;
assert.equal(reconnectReplayErrorClosed.reason, "error");
assert(reconnectReplayErrorClosed.error instanceof ShunterValidationError);
assert.equal(reconnectReplayErrorClosed.error.kind, "validation");
assert.match(reconnectReplayErrorClosed.error.message, /replay denied/);
assert.deepEqual(reconnectReplayErrorHandle.state, {
  status: "closed",
  error: reconnectReplayErrorClosed.error,
});
assert.deepEqual(reconnectReplayErrorStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "connected",
]);
await reconnectReplayErrorClient.close();

const reconnectAuthSockets = [];
const reconnectAuthStates = [];
let reconnectAuthTokenCalls = 0;
const reconnectAuthClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  token: () => {
    reconnectAuthTokenCalls += 1;
    if (reconnectAuthTokenCalls === 1) {
      return "initial-token";
    }
    throw new Error("refresh denied");
  },
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    const socket = new FakeWebSocket(url, protocols);
    reconnectAuthSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectAuthStates.push(current.status),
});
const reconnectAuthConnecting = reconnectAuthClient.connect();
await nextTurn();
assert.equal(reconnectAuthSockets[0].url, "ws://127.0.0.1:3000/subscribe?token=initial-token");
reconnectAuthSockets[0].open();
reconnectAuthSockets[0].message(identityTokenFrame().buffer);
await reconnectAuthConnecting;
const reconnectAuthHandleSubscription = reconnectAuthClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectAuthSockets[0].message(subscribeSingleAppliedFrame);
const reconnectAuthHandle = await reconnectAuthHandleSubscription;
assert.deepEqual(reconnectAuthHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectAuthSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectAuthClient.state.status, "reconnecting");
await nextTurn();
await nextTurn();
assert.equal(reconnectAuthTokenCalls, 2);
assert.equal(reconnectAuthSockets.length, 1);
assert.equal(reconnectAuthClient.state.status, "closed");
const reconnectAuthError = reconnectAuthClient.state.error;
assert(reconnectAuthError instanceof ShunterAuthError);
assert.equal(reconnectAuthError.kind, "auth");
assert.match(reconnectAuthError.message, /Token provider failed/);
const reconnectAuthClosed = await reconnectAuthHandle.closed;
assert.equal(reconnectAuthClosed.reason, "error");
assert.strictEqual(reconnectAuthClosed.error, reconnectAuthError);
assert.deepEqual(reconnectAuthHandle.state, {
  status: "closed",
  error: reconnectAuthError,
});
assert.deepEqual(reconnectAuthStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "failed",
  "closed",
]);

const reconnectFactoryFailureSockets = [];
const reconnectFactoryFailureStates = [];
let reconnectFactoryFailureCalls = 0;
const reconnectFactoryFailureClient = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterProtocol,
  reconnect: {
    enabled: true,
    maxAttempts: 1,
    initialDelayMs: 0,
    maxDelayMs: 0,
  },
  webSocketFactory: (url, protocols) => {
    reconnectFactoryFailureCalls += 1;
    if (reconnectFactoryFailureCalls > 1) {
      throw new Error("factory offline");
    }
    const socket = new FakeWebSocket(url, protocols);
    reconnectFactoryFailureSockets.push(socket);
    return socket;
  },
  onStateChange: ({ current }) => reconnectFactoryFailureStates.push(current.status),
});
const reconnectFactoryFailureConnecting = reconnectFactoryFailureClient.connect();
await nextTurn();
reconnectFactoryFailureSockets[0].open();
reconnectFactoryFailureSockets[0].message(identityTokenFrame().buffer);
await reconnectFactoryFailureConnecting;
const reconnectFactoryFailureHandleSubscription = reconnectFactoryFailureClient.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
  returnHandle: true,
  decodeRow: (row) => [...row].join("-"),
});
reconnectFactoryFailureSockets[0].message(subscribeSingleAppliedFrame);
const reconnectFactoryFailureHandle = await reconnectFactoryFailureHandleSubscription;
assert.deepEqual(reconnectFactoryFailureHandle.state, { status: "active", rows: ["1-2", "3"] });
reconnectFactoryFailureSockets[0].dispatch("close", { code: 1006, reason: "lost", wasClean: false });
assert.equal(reconnectFactoryFailureClient.state.status, "reconnecting");
await nextTurn();
await nextTurn();
assert.equal(reconnectFactoryFailureCalls, 2);
assert.equal(reconnectFactoryFailureSockets.length, 1);
assert.equal(reconnectFactoryFailureClient.state.status, "closed");
const reconnectFactoryFailureError = reconnectFactoryFailureClient.state.error;
assert.equal(reconnectFactoryFailureError.kind, "transport");
assert.match(reconnectFactoryFailureError.message, /factory offline/);
const reconnectFactoryFailureClosed = await reconnectFactoryFailureHandle.closed;
assert.equal(reconnectFactoryFailureClosed.reason, "error");
assert.strictEqual(reconnectFactoryFailureClosed.error, reconnectFactoryFailureError);
assert.deepEqual(reconnectFactoryFailureHandle.state, {
  status: "closed",
  error: reconnectFactoryFailureError,
});
assert.deepEqual(reconnectFactoryFailureStates, [
  "connecting",
  "connected",
  "reconnecting",
  "connecting",
  "failed",
  "closed",
]);
