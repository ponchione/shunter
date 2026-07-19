import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

import { launchChromium, loadPlaywright } from "./browser-helpers.mjs";

const testDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(testDir, "../../..");
const workspace = await mkdtemp(join(tmpdir(), "shunter-browser-lifecycle-"));
const dataDir = join(workspace, "data");
const fixtureBinary = join(workspace, "lifecycle-server");
const { chromium } = await loadPlaywright();

let browser;
let server;

try {
  await mkdir(dataDir);
  await runCommand("go", ["build", "-o", fixtureBinary, "./internal/browserintegration/lifecycleserver"]);
  server = await startLifecycleServer({ addr: "127.0.0.1:0", seed: true });

  browser = await launchChromium(chromium);
  const page = await browser.newPage();
  await page.exposeFunction("killLifecycleFixture", async () => {
    await killLifecycleServer(server);
  });
  await page.exposeFunction("restartLifecycleFixture", async () => {
    server = await startLifecycleServer({ addr: server.addr, seed: false });
    return server.url;
  });
  await page.goto(server.url, { waitUntil: "load" });

  const result = await page.evaluate(runSuccessfulLifecycleScenario);
  assert.equal(result.nativeWebSockets, true, "successful lifecycle must use Chromium native WebSocket objects");
  assert.equal(result.initialEpoch, 1, "initial synchronization epoch");
  assert.equal(result.reconnectedEpoch, 2, "reconnected synchronization epoch");
  assert.deepEqual(result.finalRows, [
    { id: "1", body: "initial" },
    { id: "2", body: "live-update" },
  ]);
  assert.equal(result.closedReason, "unsubscribed", "managed handle close reason");
  assert.equal(result.activeStaleFrameIgnored, true, "old-socket frame while active must be ignored");
  assert.equal(result.staleFrameIgnored, true, "old-socket frame after unsubscribe must be ignored");

  console.log("successful lifecycle browser integration passed");
} finally {
  if (browser !== undefined) {
    await browser.close();
  }
  if (server !== undefined) {
    await stopLifecycleServer(server);
  }
  await rm(workspace, { recursive: true, force: true });
}

async function runSuccessfulLifecycleScenario() {
  const {
    createShunterClient,
    decodeBsatnProduct,
    shunterProtocol,
  } = await import("/client/index.js");

  const fail = (phase, observed) => {
    throw new Error(`${phase}: ${JSON.stringify(observed)}`);
  };
  const requireInvariant = (condition, phase, observed) => {
    if (!condition) {
      fail(phase, observed);
    }
  };
  const rowColumns = [
    { name: "id", kind: "uint64" },
    { name: "body", kind: "string" },
  ];
  const decodeRow = (bytes) => decodeBsatnProduct(bytes, rowColumns, ([id, body]) => ({
    id: String(id),
    body,
  }));
  const rowsFromHandle = (handle) => {
    if (!("rows" in handle.state)) {
      return [];
    }
    return handle.state.rows.map((row) => ({ ...row }));
  };

  const sockets = [];
  const firstSocketFrames = [];
  const firstSocketMessageHandlers = [];
  const connectionStates = [];
  const initialRowsEvents = [];
  const updateEvents = [];
  let resolveLiveUpdate;
  const liveUpdate = new Promise((resolve) => {
    resolveLiveUpdate = resolve;
  });

  const client = createShunterClient({
    url: `ws://${location.host}/subscribe`,
    protocol: shunterProtocol,
    reconnect: {
      enabled: true,
      maxAttempts: 20,
      initialDelayMs: 100,
      maxDelayMs: 100,
      backoffMultiplier: 1,
      resubscribe: true,
    },
    webSocketFactory: (url, protocols) => {
      const socket = new WebSocket(url, [...protocols]);
      sockets.push(socket);
      if (sockets.length === 1) {
        const nativeAddEventListener = socket.addEventListener.bind(socket);
        nativeAddEventListener("message", (event) => {
          if (event.data instanceof ArrayBuffer) {
            firstSocketFrames.push(event.data.slice(0));
          }
        });
        socket.addEventListener = (type, listener, options) => {
          if (type === "message") {
            firstSocketMessageHandlers.push(listener);
          }
          nativeAddEventListener(type, listener, options);
        };
      }
      return socket;
    },
    onStateChange: ({ current }) => {
      connectionStates.push(current.status === "connected"
        ? {
            status: current.status,
            epoch: current.synchronization.epoch,
            synchronized: current.synchronization.synchronized,
            pendingSubscriptions: current.synchronization.pendingSubscriptions,
          }
        : { status: current.status });
    },
  });

  const waitForConnectionState = (phase, predicate) => {
    if (predicate(client.state)) {
      return Promise.resolve(client.state);
    }
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        unsubscribe();
        reject(new Error(`${phase}: timed out; observed=${JSON.stringify(client.state)}`));
      }, 5_000);
      const unsubscribe = client.onStateChange(({ current }) => {
        if (!predicate(current)) {
          return;
        }
        clearTimeout(timeout);
        unsubscribe();
        resolve(current);
      });
    });
  };

  try {
    const metadata = await client.connect();
    requireInvariant(sockets.length === 1 && sockets[0] instanceof WebSocket, "connect/native WebSocket", {
      socketCount: sockets.length,
      constructor: sockets[0]?.constructor?.name,
    });
    requireInvariant(metadata.identity?.length === 32 && metadata.connectionId?.length === 16, "connect/identity", {
      identityLength: metadata.identity?.length,
      connectionIdLength: metadata.connectionId?.length,
      state: client.state,
    });
    requireInvariant(
      client.state.status === "connected" &&
        client.state.synchronization.epoch === 1 &&
        client.state.synchronization.synchronized,
      "connect/initial synchronization",
      client.state,
    );

    const handle = await client.subscribeTable("messages", undefined, {
      requestId: 101,
      queryId: 201,
      returnHandle: true,
      decodeRow,
      onInitialRows: (rows) => {
        initialRowsEvents.push(rows.map((row) => ({ ...row })));
      },
      onUpdate: (update) => {
        updateEvents.push({
          inserts: update.inserts.map((row) => ({ ...row })),
          deletes: update.deletes.map((row) => ({ ...row })),
        });
        resolveLiveUpdate();
      },
    });
    requireInvariant(handle.epoch === 1 && handle.state.status === "active", "subscribe/initial active state", {
      epoch: handle.epoch,
      state: handle.state,
    });
    requireInvariant(JSON.stringify(rowsFromHandle(handle)) === JSON.stringify([{ id: "1", body: "initial" }]), "subscribe/initial rows", {
      rows: rowsFromHandle(handle),
      initialRowsEvents,
    });

    const insertResponse = await fetch("/control/insert?id=2&body=live-update", { method: "POST" });
    requireInvariant(insertResponse.ok, "update/control insert", {
      status: insertResponse.status,
      body: await insertResponse.text(),
    });
    await liveUpdate;
    const expectedRows = [
      { id: "1", body: "initial" },
      { id: "2", body: "live-update" },
    ];
    requireInvariant(JSON.stringify(rowsFromHandle(handle)) === JSON.stringify(expectedRows), "update/live delivery", {
      rows: rowsFromHandle(handle),
      updateEvents,
    });
    const staleFrame = firstSocketFrames.findLast((frame) => new Uint8Array(frame)[0] === 8);
    requireInvariant(staleFrame instanceof ArrayBuffer, "update/capture old-socket frame", {
      capturedTags: firstSocketFrames.map((frame) => new Uint8Array(frame)[0]),
    });

    const reconnecting = waitForConnectionState("reconnect/reconnecting barrier", (state) => state.status === "reconnecting");
    await globalThis.killLifecycleFixture();
    await reconnecting;
    requireInvariant(handle.state.status === "resynchronizing", "reconnect/handle resynchronizing", {
      clientState: client.state,
      handleEpoch: handle.epoch,
      handleState: handle.state,
    });
    requireInvariant(
      handle.state.previousEpoch === 1 &&
        handle.state.targetEpoch === 2 &&
        JSON.stringify(rowsFromHandle(handle)) === JSON.stringify(expectedRows),
      "reconnect/resynchronizing epoch and retained rows",
      { handleEpoch: handle.epoch, handleState: handle.state },
    );

    const replayPending = waitForConnectionState(
      "reconnect/replay pending barrier",
      (state) => state.status === "connected" && state.synchronization.epoch === 2 && !state.synchronization.synchronized,
    );
    const synchronized = client.whenSynchronized();
    const restartedURL = await globalThis.restartLifecycleFixture();
    requireInvariant(restartedURL === location.href, "reconnect/fixed fixture address", {
      restartedURL,
      pageURL: location.href,
    });
    const pendingState = await replayPending;
    requireInvariant(pendingState.synchronization.pendingSubscriptions === 1, "reconnect/replay subscription count", pendingState);
    const synchronization = await synchronized;
    requireInvariant(
      synchronization.epoch === 2 && synchronization.synchronized && synchronization.pendingSubscriptions === 0,
      "reconnect/synchronized epoch",
      synchronization,
    );
    requireInvariant(handle.epoch === 2 && handle.state.status === "active", "reconnect/handle active", {
      epoch: handle.epoch,
      state: handle.state,
    });
    requireInvariant(JSON.stringify(rowsFromHandle(handle)) === JSON.stringify(expectedRows), "reconnect/replayed rows without duplicates", {
      rows: rowsFromHandle(handle),
      initialRowsEvents,
    });

    const oldSocketMessageHandler = firstSocketMessageHandlers[0];
    requireInvariant(oldSocketMessageHandler !== undefined, "reconnect/capture old-socket message handler", {
      capturedHandlers: firstSocketMessageHandlers.length,
    });
    const activeCallbacksBeforeStale = {
      initial: initialRowsEvents.length,
      updates: updateEvents.length,
    };
    const staleMessageEvent = new MessageEvent("message", { data: staleFrame.slice(0) });
    if (typeof oldSocketMessageHandler === "function") {
      oldSocketMessageHandler.call(sockets[0], staleMessageEvent);
    } else {
      oldSocketMessageHandler.handleEvent(staleMessageEvent);
    }
    await Promise.resolve();
    await Promise.resolve();
    const activeCallbacksAfterStale = {
      initial: initialRowsEvents.length,
      updates: updateEvents.length,
    };
    requireInvariant(
      handle.epoch === 2 &&
        handle.state.status === "active" &&
        JSON.stringify(rowsFromHandle(handle)) === JSON.stringify(expectedRows) &&
        JSON.stringify(activeCallbacksAfterStale) === JSON.stringify(activeCallbacksBeforeStale) &&
        client.state.status === "connected" &&
        client.state.synchronization.epoch === 2 &&
        client.state.synchronization.synchronized &&
        client.state.synchronization.pendingSubscriptions === 0,
      "stale old-socket event while replayed handle active",
      {
        handleEpoch: handle.epoch,
        handleState: handle.state,
        rows: rowsFromHandle(handle),
        activeCallbacksBeforeStale,
        activeCallbacksAfterStale,
        clientState: client.state,
      },
    );

    const finalRows = rowsFromHandle(handle);
    await handle.unsubscribe();
    const closed = await handle.closed;
    requireInvariant(closed.reason === "unsubscribed" && handle.state.status === "closed", "unsubscribe/completion", {
      closed,
      handleState: handle.state,
    });

    const callbacksBeforeStale = {
      initial: initialRowsEvents.length,
      updates: updateEvents.length,
    };
    sockets[0].dispatchEvent(new MessageEvent("message", { data: staleFrame.slice(0) }));
    await Promise.resolve();
    await Promise.resolve();
    const callbacksAfterStale = {
      initial: initialRowsEvents.length,
      updates: updateEvents.length,
    };
    requireInvariant(
      handle.state.status === "closed" &&
        JSON.stringify(callbacksAfterStale) === JSON.stringify(callbacksBeforeStale) &&
        client.state.status === "connected",
      "stale old-socket event after unsubscribe",
      {
        handleState: handle.state,
        callbacksBeforeStale,
        callbacksAfterStale,
        clientState: client.state,
      },
    );

    return {
      nativeWebSockets: sockets.length >= 2 && sockets.every((socket) => socket instanceof WebSocket),
      initialEpoch: 1,
      reconnectedEpoch: synchronization.epoch,
      finalRows,
      closedReason: closed.reason,
      activeStaleFrameIgnored: JSON.stringify(activeCallbacksAfterStale) === JSON.stringify(activeCallbacksBeforeStale),
      staleFrameIgnored: JSON.stringify(callbacksAfterStale) === JSON.stringify(callbacksBeforeStale),
      connectionStates,
    };
  } finally {
    await client.dispose();
  }
}

async function runCommand(command, args) {
  const child = spawn(command, args, {
    cwd: repoRoot,
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stdout = [];
  const stderr = [];
  child.stdout.on("data", (chunk) => stdout.push(chunk));
  child.stderr.on("data", (chunk) => stderr.push(chunk));
  const [code, signal] = await once(child, "exit");
  if (code !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed: code=${code} signal=${signal}\n${Buffer.concat(stdout)}\n${Buffer.concat(stderr)}`);
  }
}

async function startLifecycleServer({ addr, seed }) {
  const child = spawn(fixtureBinary, [], {
    cwd: repoRoot,
    env: {
      ...process.env,
      SHUNTER_BROWSER_LIFECYCLE_ADDR: addr,
      SHUNTER_BROWSER_LIFECYCLE_DATA_DIR: dataDir,
      SHUNTER_BROWSER_LIFECYCLE_SEED: seed ? "1" : "0",
    },
    stdio: ["pipe", "pipe", "pipe"],
  });
  child.stderr.setEncoding("utf8");
  let stderr = "";
  child.stderr.on("data", (chunk) => {
    stderr += chunk;
  });

  const lineReader = createInterface({ input: child.stdout });
  const exit = once(child, "exit").then(([code, signal]) => ({ type: "exit", code, signal }));
  const firstLine = once(lineReader, "line").then(([line]) => ({ type: "line", line }));
  const startup = await Promise.race([firstLine, exit]);
  lineReader.close();
  if (startup.type === "exit") {
    throw new Error(`lifecycle fixture exited before startup: code=${startup.code} signal=${startup.signal}\n${stderr}`);
  }

  let info;
  try {
    info = JSON.parse(startup.line);
  } catch (error) {
    throw new Error(`lifecycle fixture wrote invalid startup JSON ${JSON.stringify(startup.line)}: ${error}\n${stderr}`);
  }
  if (typeof info.url !== "string" || info.url === "" || typeof info.addr !== "string" || info.addr === "") {
    throw new Error(`lifecycle fixture startup JSON missing url/addr: ${startup.line}`);
  }
  return { process: child, url: info.url, addr: info.addr, stderr: () => stderr };
}

async function killLifecycleServer(activeServer) {
  const child = activeServer.process;
  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  const exit = once(child, "exit");
  child.kill("SIGKILL");
  await exit;
}

async function stopLifecycleServer(activeServer) {
  const child = activeServer.process;
  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  child.stdin.end();
  const exit = once(child, "exit");
  let timeout;
  const result = await Promise.race([
    exit.then(() => "exit"),
    new Promise((resolve) => {
      timeout = setTimeout(() => resolve("timeout"), 5_000);
    }),
  ]);
  clearTimeout(timeout);
  if (result === "timeout") {
    child.kill("SIGTERM");
    await exit;
  }
  if (child.exitCode !== 0 && child.signalCode === null) {
    throw new Error(`lifecycle fixture shutdown failed with code ${child.exitCode}\n${activeServer.stderr()}`);
  }
}
