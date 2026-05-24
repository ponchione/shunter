import {
  assertGeneratedContractCompatible,
  createShunterClient,
  type ConnectionState,
} from "@shunter/client";
import {
  callSendSystemMessageProcedureTyped,
  callSendMessageTyped,
  shunterContract,
  type ShunterSubprotocol,
  subscribeLiveMessagesHandle,
  subscribeMessageEventsInserts,
} from "./generated/hosted_chat";

const shunterUrl = "ws://127.0.0.1:3000/subscribe";
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

const metadata = await client.connect();
const negotiatedSubprotocol = metadata.subprotocol as ShunterSubprotocol;
console.log(`connected with ${negotiatedSubprotocol}`);

const unsubscribeMessageEvents = await subscribeMessageEventsInserts(
  client.subscribeTable,
  (event) => {
    console.log(`event ${event.row.author}: ${event.row.body}`);
  },
);

const liveMessages = await subscribeLiveMessagesHandle(client.subscribeDeclaredView, {
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

await callSendMessageTyped(client.callReducer, {
  author: "Ada",
  body: "hello from the TypeScript client",
});

await callSendSystemMessageProcedureTyped(client.callProcedure, {
  body: "hello from the TypeScript procedure client",
});

await unsubscribeMessageEvents();
await liveMessages.unsubscribe();
await client.close();
console.log(`observed ${states.length} connection states`);
