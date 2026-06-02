import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { dirname } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

import {
  createShunterClient,
  decodeDeclaredQueryResult,
  shunterProtocol,
} from "../.tmp_runtime_test/src/index.js";
import {
  decodeFlatValuesRow,
  tableRowDecoders,
} from "../.tmp_runtime_test/test/fixtures/flat_type_index_canary.js";

const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const repoRoot = dirname(dirname(packageRoot));

const server = await startHostedCanaryServer();
try {
  const writer = createShunterClient({
    url: server.url,
    protocol: shunterProtocol,
  });
  const reader = createShunterClient({
    url: server.url,
    protocol: shunterProtocol,
  });
  try {
    await Promise.all([writer.connect(), reader.connect()]);

    const alpha = { id: 1n, label: "alpha", bucket: "active", seq: 10n, note: "alpha note" };
    const betaInactive = { id: 2n, label: "beta", bucket: "inactive", seq: 20n, note: null };
    const betaActive = { id: 2n, label: "beta_active", bucket: "active", seq: 15n, note: null };

    await callCanaryReducer(writer, "insert_flat_value", alpha, 0x1001);
    await callCanaryReducer(writer, "insert_flat_value", betaInactive, 0x1002);

    const initialQuery = await runDecodedCanaryQuery(reader, 0x2001);
    assertRows(initialQuery, [expectedCanaryRow(alpha)], "initial declared query");

    const initialRows = [];
    let resolveBetaUpdate;
    const nextBetaUpdate = new Promise((resolve) => {
      resolveBetaUpdate = resolve;
    });
    const handle = await reader.subscribeDeclaredView("active_flat_values_live", {
      requestId: 0x3001,
      queryId: 0x3002,
      returnHandle: true,
      decodeRow: decodeFlatValuesRow,
      onInitialRows: (rows) => initialRows.push(...rows),
      onUpdate: (update) => {
        if (update.inserts.some((row) => row.id === betaActive.id)) {
          resolveBetaUpdate(update);
        }
      },
    });
    assertRows(initialRows, [expectedCanaryRow(alpha)], "declared view initial rows");
    assertRows(handle.state.rows, [expectedCanaryRow(alpha)], "declared view handle initial rows");

    await callCanaryReducer(writer, "update_flat_value", betaActive, 0x1003);
    const betaUpdate = await withTimeout(nextBetaUpdate, 5_000, "declared view beta update");
    assertRows(betaUpdate.inserts, [expectedCanaryRow(betaActive)], "declared view beta inserts");
    assertRows(betaUpdate.deletes, [], "declared view beta deletes");

    const finalQuery = await runDecodedCanaryQuery(reader, 0x2002);
    assertRows(finalQuery, [expectedCanaryRow(alpha), expectedCanaryRow(betaActive)], "final declared query");

    await handle.unsubscribe();
  } finally {
    await Promise.allSettled([
      reader.close(),
      writer.close(),
    ]);
  }
} finally {
  await server.stop();
}

async function startHostedCanaryServer() {
  const child = spawn("go", ["run", "./typescript/client/test/fixtures/hosted_type_index_canary"], {
    cwd: repoRoot,
    stdio: ["pipe", "pipe", "pipe"],
  });
  let stderr = "";
  child.stderr.setEncoding("utf8");
  child.stderr.on("data", (chunk) => {
    stderr += chunk;
  });
  const lines = createInterface({ input: child.stdout });
  const startup = new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(`hosted canary server did not start before timeout${formatStderr(stderr)}`));
    }, 20_000);
    child.once("error", (error) => {
      clearTimeout(timer);
      reject(error);
    });
    child.once("exit", (code, signal) => {
      clearTimeout(timer);
      reject(new Error(`hosted canary server exited during startup code=${code} signal=${signal}${formatStderr(stderr)}`));
    });
    lines.once("line", (line) => {
      clearTimeout(timer);
      try {
        const ready = JSON.parse(line);
        if (typeof ready.url !== "string") {
          throw new Error(`missing url in ready message ${line}`);
        }
        resolve(ready);
      } catch (error) {
        reject(error);
      }
    });
  });
  const ready = await startup;
  return {
    url: ready.url,
    stop: async () => {
      child.stdin.end();
      await new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          child.kill("SIGKILL");
          reject(new Error(`hosted canary server did not stop before timeout${formatStderr(stderr)}`));
        }, 5_000);
        child.once("exit", (code, signal) => {
          clearTimeout(timer);
          if (code === 0 || signal === "SIGTERM") {
            resolve();
            return;
          }
          reject(new Error(`hosted canary server exited code=${code} signal=${signal}${formatStderr(stderr)}`));
        });
      });
    },
  };
}

async function callCanaryReducer(client, name, input, requestId) {
  await client.callReducer(name, encodeCanaryInput(input), { requestId });
}

function encodeCanaryInput(input) {
  return new TextEncoder().encode(JSON.stringify({
    id: Number(input.id),
    label: input.label,
    bucket: input.bucket,
    seq: Number(input.seq),
    note: input.note,
  }));
}

async function runDecodedCanaryQuery(client, requestId) {
  const raw = await client.runDeclaredQuery("active_flat_values", { requestId });
  const decoded = decodeDeclaredQueryResult("active_flat_values", raw, { tableDecoders: tableRowDecoders });
  assert.equal(decoded.tables.length, 1, "decoded declared query table count");
  assert.equal(decoded.tables[0].tableName, "flat_values", "decoded declared query table name");
  return decoded.tables[0].rows;
}

function expectedCanaryRow(input) {
  const idNumber = Number(input.id);
  const seqNumber = Number(input.seq);
  return {
    id: input.id,
    label: input.label,
    bucket: input.bucket,
    seq: input.seq,
    flag: input.id % 2n === 1n,
    i8: -idNumber,
    u8: idNumber + 10,
    i16: -(idNumber * 2),
    u16: idNumber * 2 + 10,
    i32: -(idNumber * 3),
    u32: idNumber * 3 + 10,
    i64: -(input.id * 4n),
    u64: input.id * 4n + 10n,
    i128: wide128(input.id, input.seq + 100n),
    u128: wide128(input.id + 200n, input.seq + 200n),
    i256: wide256(input.id, input.seq + 300n, input.id + 300n, input.seq + 301n),
    u256: wide256(input.id + 400n, input.seq + 400n, input.id + 401n, input.seq + 401n),
    f32: idNumber + 0.25,
    f64: idNumber + 0.5,
    createdAt: 1_700_000_000_000_000n + input.seq,
    ttl: input.seq * 1_000n,
    uuid: canaryUUID(input.id),
    blob: new Uint8Array([idNumber, seqNumber, 0xa5]),
    metadata: { bucket: input.bucket, id: idNumber, label: input.label },
    tags: [input.bucket, input.label, `seq-${input.seq}`],
    optionalNote: input.note,
  };
}

function assertRows(actual, expected, label) {
  assert.equal(actual.length, expected.length, `${label} row count`);
  const actualByID = new Map(actual.map((row) => [row.id, row]));
  for (const row of expected) {
    const got = actualByID.get(row.id);
    assert(got, `${label} missing row id ${row.id}`);
    assertRow(got, row, `${label} row ${row.id}`);
  }
}

function assertRow(actual, expected, label) {
  assert.equal(actual.id, expected.id, `${label} id`);
  assert.equal(actual.label, expected.label, `${label} label`);
  assert.equal(actual.bucket, expected.bucket, `${label} bucket`);
  assert.equal(actual.seq, expected.seq, `${label} seq`);
  assert.equal(actual.flag, expected.flag, `${label} flag`);
  assert.equal(actual.i8, expected.i8, `${label} i8`);
  assert.equal(actual.u8, expected.u8, `${label} u8`);
  assert.equal(actual.i16, expected.i16, `${label} i16`);
  assert.equal(actual.u16, expected.u16, `${label} u16`);
  assert.equal(actual.i32, expected.i32, `${label} i32`);
  assert.equal(actual.u32, expected.u32, `${label} u32`);
  assert.equal(actual.i64, expected.i64, `${label} i64`);
  assert.equal(actual.u64, expected.u64, `${label} u64`);
  assert.equal(actual.i128, expected.i128, `${label} i128`);
  assert.equal(actual.u128, expected.u128, `${label} u128`);
  assert.equal(actual.i256, expected.i256, `${label} i256`);
  assert.equal(actual.u256, expected.u256, `${label} u256`);
  assert.equal(actual.f32, expected.f32, `${label} f32`);
  assert.equal(actual.f64, expected.f64, `${label} f64`);
  assert.equal(actual.createdAt, expected.createdAt, `${label} createdAt`);
  assert.equal(actual.ttl, expected.ttl, `${label} ttl`);
  assert.equal(actual.uuid, expected.uuid, `${label} uuid`);
  assert.deepEqual(actual.blob, expected.blob, `${label} blob`);
  assert.deepEqual(actual.metadata, expected.metadata, `${label} metadata`);
  assert.deepEqual(actual.tags, expected.tags, `${label} tags`);
  assert.equal(actual.optionalNote, expected.optionalNote, `${label} optionalNote`);
}

function wide128(hi, lo) {
  return (hi << 64n) + lo;
}

function wide256(w0, w1, w2, w3) {
  return (w0 << 192n) + (w1 << 128n) + (w2 << 64n) + w3;
}

function canaryUUID(id) {
  const bytes = new Uint8Array(16);
  bytes.set([0x10, 0x20, 0x30, 0x40]);
  bytes[8] = Number((id >> 24n) & 0xffn);
  bytes[9] = Number((id >> 16n) & 0xffn);
  bytes[10] = Number((id >> 8n) & 0xffn);
  bytes[11] = Number(id & 0xffn);
  bytes[15] = Number((id + 0x40n) & 0xffn);
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

async function withTimeout(promise, ms, label) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error(`${label} timed out after ${ms}ms`)), ms);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

function formatStderr(stderr) {
  return stderr === "" ? "" : ` stderr=${JSON.stringify(stderr)}`;
}
