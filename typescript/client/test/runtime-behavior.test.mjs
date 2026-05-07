import assert from "node:assert/strict";
import {
  SHUNTER_SUBPROTOCOL_V1,
  ShunterAuthError,
  ShunterClosedClientError,
  ShunterProtocolMismatchError,
  assertProtocolCompatible,
  checkProtocolCompatibility,
  createShunterClient,
  createSubscriptionHandle,
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

  open(protocol = this.protocol) {
    this.protocol = protocol;
    this.dispatch("open", {});
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
const metadata = await connecting;
assert.equal(metadata.subprotocol, SHUNTER_SUBPROTOCOL_V1);
assert.equal(client.state.status, "connected");
await client.close();
await client.close();
await client.dispose();
assert.equal(sockets[0].closeCalls.length, 1);
assert.deepEqual(clientStates, ["connecting", "connected", "closing", "closed"]);

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
