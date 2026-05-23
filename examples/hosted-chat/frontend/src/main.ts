import { createShunterClient } from "@shunter/client";
import {
  callSendSystemMessageProcedureTyped,
  callSendMessageTyped,
  shunterContract,
  subscribeLiveMessagesHandle,
} from "./generated/hosted_chat";

const client = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: shunterContract.protocol,
  contract: shunterContract,
});

await client.connect();

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

await liveMessages.unsubscribe();
await client.close();
