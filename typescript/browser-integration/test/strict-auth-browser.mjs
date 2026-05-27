import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createInterface } from "node:readline";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const closePolicy = 1008;
const closeReasonAuthRejected = "auth-token rejected by admission";
const testDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(testDir, "../../..");
const { chromium } = await loadPlaywright();

const server = await startStrictAuthServer();
let browser;

try {
  browser = await launchChromium();
  const page = await browser.newPage();
  await page.goto(server.url, { waitUntil: "load" });

  for (const authCase of [
    { name: "missing token", tokenQuery: "" },
    { name: "invalid token", tokenQuery: "?token=not-a-jwt" },
  ]) {
    const result = await observeNativeWebSocketAuthClose(page, authCase.tokenQuery);
    assert.equal(result.type, "close", `${authCase.name}: native browser WebSocket should receive a close event`);
    assert.equal(result.opened, true, `${authCase.name}: browser should observe open before auth close`);
    assert.equal(result.code, closePolicy, `${authCase.name}: browser close code`);
    assert.equal(result.reason, closeReasonAuthRejected, `${authCase.name}: browser close reason`);
  }

  for (const authCase of [
    { name: "missing token", token: undefined },
    { name: "invalid token", token: "not-a-jwt" },
  ]) {
    const result = await observeClientAuthClassification(page, authCase.token);
    assert.equal(result.type, "rejected", `${authCase.name}: client connect should reject`);
    assert.equal(result.isAuthError, true, `${authCase.name}: rejection should be ShunterAuthError`);
    assert.equal(result.name, "ShunterAuthError", `${authCase.name}: error name`);
    assert.equal(result.kind, "auth", `${authCase.name}: error kind`);
    assert.equal(result.code, String(closePolicy), `${authCase.name}: error code`);
    assert.equal(result.details?.reason, closeReasonAuthRejected, `${authCase.name}: error reason detail`);
    assert.equal(typeof result.details?.wasClean, "boolean", `${authCase.name}: wasClean detail`);
    assert.equal(result.state, "failed", `${authCase.name}: client state`);
  }

  console.log("strict-auth browser integration passed");
} finally {
  if (browser !== undefined) {
    await browser.close();
  }
  await stopStrictAuthServer(server.process);
}

async function startStrictAuthServer() {
  const child = spawn("go", ["run", "./internal/browserintegration/strictauthserver"], {
    cwd: repoRoot,
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
    throw new Error(`strict-auth fixture exited before startup: code=${startup.code} signal=${startup.signal}\n${stderr}`);
  }

  let info;
  try {
    info = JSON.parse(startup.line);
  } catch (error) {
    throw new Error(`strict-auth fixture wrote invalid startup JSON ${JSON.stringify(startup.line)}: ${error}\n${stderr}`);
  }
  if (typeof info.url !== "string" || info.url === "") {
    throw new Error(`strict-auth fixture startup JSON missing url: ${startup.line}`);
  }
  return { process: child, url: info.url };
}

async function stopStrictAuthServer(child) {
  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  child.stdin.end();
  const exit = once(child, "exit");
  const timeout = new Promise((resolve) => {
    setTimeout(resolve, 2_000);
  });
  if (await Promise.race([exit.then(() => "exit"), timeout.then(() => "timeout")]) === "timeout") {
    child.kill("SIGTERM");
    await once(child, "exit");
  }
}

async function launchChromium() {
  try {
    return await chromium.launch({ channel: "chrome", headless: true, args: ["--no-sandbox"] });
  } catch (channelError) {
    try {
      return await chromium.launch({ headless: true, args: ["--no-sandbox"] });
    } catch (bundledError) {
      if (
        String(bundledError).includes("Executable doesn't exist") ||
        String(bundledError).includes("Please run the following command")
      ) {
        throw new Error(
          "Playwright Chromium is not installed. Run `npm --prefix typescript/browser-integration run install:browsers`.\n" +
            bundledError,
        );
      }
      throw new Error(`launch chrome failed: ${channelError}\nlaunch bundled chromium failed: ${bundledError}`);
    }
  }
}

async function loadPlaywright() {
  try {
    return await import("playwright");
  } catch (error) {
    if (error?.code === "ERR_MODULE_NOT_FOUND") {
      throw new Error("Playwright is not installed. Run `npm install --prefix typescript/browser-integration`.");
    }
    throw error;
  }
}

async function observeNativeWebSocketAuthClose(page, tokenQuery) {
  return page.evaluate(async ({ tokenQuery, closeReasonAuthRejected }) => {
    return await new Promise((resolve) => {
      let opened = false;
      let errors = 0;
      let finished = false;
      const finish = (value) => {
        if (finished) {
          return;
        }
        finished = true;
        resolve(value);
      };

      const ws = new WebSocket(`ws://${location.host}/subscribe${tokenQuery}`, ["v1.bsatn.shunter"]);
      ws.addEventListener("open", () => {
        opened = true;
      });
      ws.addEventListener("error", () => {
        errors += 1;
      });
      ws.addEventListener("close", (event) => {
        finish({
          type: "close",
          opened,
          code: event.code,
          reason: event.reason,
          wasClean: event.wasClean,
          errors,
          readyState: ws.readyState,
        });
      });
      setTimeout(() => {
        finish({
          type: "timeout",
          opened,
          code: 0,
          reason: "",
          expectedReason: closeReasonAuthRejected,
          wasClean: false,
          errors,
          readyState: ws.readyState,
        });
      }, 3_000);
    });
  }, { tokenQuery, closeReasonAuthRejected });
}

async function observeClientAuthClassification(page, token) {
  return page.evaluate(async ({ token }) => {
    const { createShunterClient, shunterProtocol, ShunterAuthError } = await import("/client/index.js");
    const client = createShunterClient({
      url: `ws://${location.host}/subscribe`,
      protocol: shunterProtocol,
      token,
      reconnect: false,
    });

    try {
      await client.connect();
      return {
        type: "resolved",
        state: client.state.status,
      };
    } catch (error) {
      return {
        type: "rejected",
        name: error?.name,
        kind: error?.kind,
        code: error?.code,
        message: error?.message,
        isAuthError: error instanceof ShunterAuthError,
        details: error?.details,
        state: client.state.status,
      };
    }
  }, { token });
}
