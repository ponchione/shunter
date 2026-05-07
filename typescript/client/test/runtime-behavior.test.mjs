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
  ShunterValidationError,
  assertProtocolCompatible,
  checkProtocolCompatibility,
  createShunterClient,
  createSubscriptionHandle,
  decodeIdentityTokenFrame,
  decodeOneOffQueryResponseFrame,
  decodeRawDeclaredQueryResult,
  decodeReducerCallResult,
  decodeRowList,
  decodeSubscribeSingleAppliedFrame,
  decodeSubscribeMultiAppliedFrame,
  decodeSubscriptionErrorFrame,
  decodeTransactionUpdateLightFrame,
  decodeTransactionUpdateFrame,
  decodeUnsubscribeSingleAppliedFrame,
  decodeUnsubscribeMultiAppliedFrame,
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
const subscriptionError = decodeSubscriptionErrorFrame(subscriptionErrorFrame);
assert.equal(subscriptionErrorFrame[0], SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR);
assert.equal(subscriptionError.requestId, 0x41424344);
assert.equal(subscriptionError.queryId, 0x51525354);
assert.equal(subscriptionError.tableId, 0x61626364);
assert.equal(subscriptionError.error, "denied");

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
const tableLightUpdateFrame = bytesFromHex(
  "083433323101000000141312110500000075736572730f000000020000000200000001020100000003020000000405",
);
const unsubscribeTableAppliedFrame = bytesFromHex(
  "030200000000000000000000001413121100",
);
const tableSubscription = client.subscribeTable("users", undefined, {
  requestId: 0x01020304,
  queryId: 0x11121314,
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
sockets[0].message(tableLightUpdateFrame);
assert.equal(tableRawUpdates.length, 1);
assert.equal(tableRawUpdates[0].queryId, 0x11121314);
assert.deepEqual(tableRawUpdates[0].insertRowBytes.map((row) => [...row]), [[1, 2], [3]]);
assert.equal(tableRawUpdates[0].deleteRowBytes, undefined);
const unsubscribeTableResult = unsubscribeTable();
assert.equal(unsubscribeTable(), unsubscribeTableResult);
assert.equal(sockets[0].sent.length, 10);
assert.deepEqual(
  sockets[0].sent[9],
  encodeUnsubscribeSingleRequest(0x11121314, { requestId: 2 }).frame,
);
sockets[0].message(tableLightUpdateFrame);
assert.equal(tableRawUpdates.length, 2);
sockets[0].message(unsubscribeTableAppliedFrame);
await unsubscribeTableResult;
sockets[0].message(tableLightUpdateFrame);
assert.equal(tableRawUpdates.length, 2);

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
