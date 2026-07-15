import {
  SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED,
  SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT,
  createShunterClient,
  shunterProtocol,
} from "../dist/index.js";

class BenchmarkWebSocket {
  constructor(_url, protocols) {
    this.protocol = protocols[0] ?? "";
    this.binaryType = "arraybuffer";
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

  send() {}

  close(code = 1000, reason = "") {
    this.dispatch("close", { code, reason, wasClean: true });
  }

  dispatch(type, event) {
    for (const listener of [...(this.listeners.get(type) ?? [])]) {
      listener(event);
    }
  }

  open() {
    this.dispatch("open", {});
  }

  message(frame) {
    this.dispatch("message", { data: frame.buffer });
  }
}

function writeUint32(frame, offset, value) {
  new DataView(frame.buffer).setUint32(offset, value, true);
  return offset + 4;
}

function identityFrame() {
  const frame = new Uint8Array(1 + 32 + 4 + 16);
  frame[0] = 1;
  return frame;
}

function rowBytes(value) {
  const row = new Uint8Array(rowSize);
  writeUint32(row, 0, value);
  for (let index = 4; index < row.length; index += 1) {
    row[index] = (value + index) & 0xff;
  }
  return row;
}

function rowList(rows) {
  const frame = new Uint8Array(4 + rows.reduce((size, row) => size + 4 + row.length, 0));
  let offset = writeUint32(frame, 0, rows.length);
  for (const row of rows) {
    offset = writeUint32(frame, offset, row.length);
    frame.set(row, offset);
    offset += row.length;
  }
  return frame;
}

function subscribeAppliedFrame(requestId, queryId, rows) {
  const tableName = new TextEncoder().encode("bench_rows");
  const encodedRows = rowList(rows);
  const frame = new Uint8Array(1 + 4 + 8 + 4 + 4 + tableName.length + 4 + encodedRows.length);
  let offset = 0;
  frame[offset] = SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED;
  offset += 1;
  offset = writeUint32(frame, offset, requestId);
  offset += 8;
  offset = writeUint32(frame, offset, queryId);
  offset = writeUint32(frame, offset, tableName.length);
  frame.set(tableName, offset);
  offset += tableName.length;
  offset = writeUint32(frame, offset, encodedRows.length);
  frame.set(encodedRows, offset);
  return frame;
}

function updateFrame(queryId, insert, remove) {
  const tableName = new TextEncoder().encode("bench_rows");
  const inserts = rowList([insert]);
  const deletes = rowList([remove]);
  const frame = new Uint8Array(
    1 + 4 + 4 + 4 + 4 + tableName.length + 4 + inserts.length + 4 + deletes.length,
  );
  let offset = 0;
  frame[offset] = SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT;
  offset += 1;
  offset = writeUint32(frame, offset, 0);
  offset = writeUint32(frame, offset, 1);
  offset = writeUint32(frame, offset, queryId);
  offset = writeUint32(frame, offset, tableName.length);
  frame.set(tableName, offset);
  offset += tableName.length;
  offset = writeUint32(frame, offset, inserts.length);
  frame.set(inserts, offset);
  offset += inserts.length;
  offset = writeUint32(frame, offset, deletes.length);
  frame.set(deletes, offset);
  return frame;
}

const rowCount = Number.parseInt(process.env.SHUNTER_BENCH_ROWS ?? "10000", 10);
const deltaCount = Number.parseInt(process.env.SHUNTER_BENCH_DELTAS ?? "200", 10);
const rowSize = Number.parseInt(process.env.SHUNTER_BENCH_ROW_BYTES ?? "128", 10);
const requestId = 1;
const queryId = 2;
let socket;
const client = createShunterClient({
  url: "ws://127.0.0.1/benchmark",
  protocol: shunterProtocol,
  webSocketFactory: (_url, protocols) => {
    socket = new BenchmarkWebSocket(_url, protocols);
    return socket;
  },
});

const connecting = client.connect();
await new Promise((resolve) => setTimeout(resolve, 0));
socket.open();
socket.message(identityFrame());
await connecting;

const initialRows = Array.from({ length: rowCount }, (_, index) => rowBytes(index));
const subscription = client.subscribeTable("bench_rows", undefined, {
  requestId,
  queryId,
  returnHandle: true,
  decodeRow: (row) => new DataView(row.buffer, row.byteOffset, row.byteLength).getUint32(0, true),
});
socket.message(subscribeAppliedFrame(requestId, queryId, initialRows));
const handle = await subscription;
const replaceOwnedRows = Object.getOwnPropertySymbols(handle).find(
  (symbol) => symbol.description === "replaceOwnedRows",
);
if (replaceOwnedRows === undefined) {
  throw new Error("managed subscription handle has no owned cache publication path");
}
let snapshotAllocations = 0;
let snapshotRowSlots = 0;
const originalReplaceOwnedRows = handle[replaceOwnedRows];
handle[replaceOwnedRows] = function benchmarkReplaceOwnedRows(rows, epoch) {
  snapshotAllocations += 1;
  snapshotRowSlots += rows.length;
  return originalReplaceOwnedRows.call(this, rows, epoch);
};

const frames = Array.from({ length: deltaCount }, (_, index) =>
  updateFrame(queryId, rowBytes(rowCount + index), rowBytes(index))
);
globalThis.gc?.();
const heapBefore = process.memoryUsage().heapUsed;
let heapHighWater = heapBefore;
const started = performance.now();
for (const frame of frames) {
  socket.message(frame);
  heapHighWater = Math.max(heapHighWater, process.memoryUsage().heapUsed);
}
const elapsedMs = performance.now() - started;
globalThis.gc?.();
const heapAfter = process.memoryUsage().heapUsed;

if (handle.state.status !== "active" || handle.state.rows.length !== rowCount) {
  throw new Error(`unexpected final cache size: ${handle.state.status} ${handle.state.rows?.length}`);
}
if (snapshotAllocations !== deltaCount) {
  throw new Error(`snapshot publications=${snapshotAllocations}, want one per delta (${deltaCount})`);
}

console.log(JSON.stringify({
  rowCount,
  rowSize,
  deltaCount,
  elapsedMs: Number(elapsedMs.toFixed(3)),
  operationsPerSecond: Number((deltaCount * 1000 / elapsedMs).toFixed(1)),
  snapshotAllocations,
  snapshotRowSlots,
  heapHighWaterDeltaBytes: heapHighWater - heapBefore,
  retainedHeapDeltaBytes: heapAfter - heapBefore,
}));

await client.close();
