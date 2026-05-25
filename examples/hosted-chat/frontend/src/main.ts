import {
  assertGeneratedContractCompatible,
  createShunterClient,
  type ConnectionState,
} from "@shunter/client";
import {
  createModuleClient,
  shunterContract,
  type ShunterSubprotocol,
} from "./generated/hosted_chat";

const shunterUrl = "ws://127.0.0.1:3000/subscribe";
const cleanupTimeoutMs = 5000;
const states: ConnectionState<typeof shunterContract.protocol>[] = [];

function readAuthToken(): string {
  const token = globalThis.localStorage?.getItem("hosted-chat-token");
  if (token === undefined || token === null || token === "") {
    throw new Error("hosted-chat-token is not configured");
  }
  return token;
}

const hasAuthToken = (globalThis.localStorage?.getItem("hosted-chat-token") ?? "") !== "";

assertGeneratedContractCompatible(shunterContract, {
  moduleName: "hosted_chat",
  moduleVersion: "v0.1.0",
});

const client = createShunterClient({
  url: shunterUrl,
  protocol: shunterContract.protocol,
  contract: shunterContract,
  ...(hasAuthToken ? { token: readAuthToken } : {}),
  reconnect: {
    enabled: true,
    maxAttempts: 3,
    initialDelayMs: 250,
    maxDelayMs: 2000,
  },
  onStateChange: ({ current }) => {
    states.push(current);
  },
});
const chat = createModuleClient(client);

let unsubscribeMessageEvents: Awaited<ReturnType<typeof chat.events.messageEvents.onInsert>> | undefined;
let liveMessages: Awaited<ReturnType<typeof chat.views.liveMessages.handle>> | undefined;
let runError: unknown;
const cleanupErrors: unknown[] = [];

async function runCleanupStep(label: string, cleanup: () => void | Promise<void>): Promise<void> {
  let timeout: ReturnType<typeof globalThis.setTimeout> | undefined;
  const operation = Promise.resolve().then(cleanup);
  operation.catch(() => {
    // The race below reports the first cleanup failure. This consumes any later
    // rejection when a timeout wins first.
  });
  try {
    await Promise.race([
      operation,
      new Promise<never>((_, reject) => {
        timeout = globalThis.setTimeout(() => {
          reject(new Error(`${label} cleanup did not finish within ${cleanupTimeoutMs}ms`));
        }, cleanupTimeoutMs);
      }),
    ]);
  } finally {
    if (timeout !== undefined) {
      globalThis.clearTimeout(timeout);
    }
  }
}

try {
  const metadata = await client.connect();
  const negotiatedSubprotocol = metadata.subprotocol as ShunterSubprotocol;
  console.log(`connected with ${negotiatedSubprotocol}`);

  unsubscribeMessageEvents = await chat.events.messageEvents.onInsert((event) => {
    console.log(`event ${event.row.author}: ${event.row.body}`);
  });

  liveMessages = await chat.views.liveMessages.handle({
    returnHandle: true,
    onInitialRows(rows) {
      for (const row of rows) {
        console.log(`${row.author}: ${row.body}`);
      }
    },
    onUpdate(update) {
      for (const row of update.inserts) {
        console.log(`${row.author}: ${row.body}`);
      }
    },
  });

  await chat.reducers.sendMessage.call({
    author: "Ada",
    body: "hello from the TypeScript client",
  });

  await chat.procedures.sendSystemMessage.call({
    body: "hello from the TypeScript procedure client",
  });
} catch (error) {
  runError = error;
}

if (unsubscribeMessageEvents !== undefined) {
  try {
    await runCleanupStep("message event subscription", unsubscribeMessageEvents);
  } catch (error) {
    cleanupErrors.push(error);
  }
}
if (liveMessages !== undefined) {
  try {
    const handle = liveMessages;
    await runCleanupStep("live messages subscription", async () => {
      await handle.unsubscribe();
      const closed = await handle.closed;
      if (closed.reason === "error") {
        throw closed.error;
      }
      if (closed.reason !== "unsubscribed") {
        throw new Error(`live messages subscription cleanup closed with reason ${closed.reason}`);
      }
    });
  } catch (error) {
    cleanupErrors.push(error);
  }
}
try {
  await runCleanupStep("client", () => client.close());
} catch (error) {
  cleanupErrors.push(error);
}
console.log(`observed ${states.length} connection states`);

if (runError !== undefined) {
  throw runError;
}
if (cleanupErrors.length > 0) {
  throw cleanupErrors[0];
}
