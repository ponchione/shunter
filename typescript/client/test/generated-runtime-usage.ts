import {
  SHUNTER_SUBPROTOCOL_V1,
  ShunterAuthError,
  ShunterProtocolMismatchError,
  shunterProtocol as runtimeProtocol,
} from "../src/index";
import type {
  ConnectionState,
  ProtocolMetadata,
  RuntimeBindings,
  ShunterErrorKind,
  SubscriptionHandle,
} from "../src/index";
import {
  callCreateMessage,
  queryRecentMessages,
  shunterProtocol as generatedProtocol,
  subscribeLiveMessageCount,
  subscribeLiveMessageProjection,
  subscribeMessages,
} from "../../../codegen/testdata/v1_module_contract";
import type {
  DeclaredQueryRunner,
  DeclaredViewSubscriber,
  ExecutableQueryName,
  ExecutableViewName,
  MessagesRow,
  ReducerCaller,
  ReducerName,
  SubscriptionUnsubscribe,
  TableName,
  TableRows,
  TableSubscriber,
} from "../../../codegen/testdata/v1_module_contract";

const generatedProtocolMetadata: ProtocolMetadata = generatedProtocol;
const runtimeProtocolMetadata: ProtocolMetadata = runtimeProtocol;
const selectedSubprotocol: typeof SHUNTER_SUBPROTOCOL_V1 =
  generatedProtocol.defaultSubprotocol;

const connectedState: ConnectionState<typeof generatedProtocol> = {
  status: "connected",
  metadata: {
    protocol: generatedProtocol,
    subprotocol: selectedSubprotocol,
  },
};

const authError = new ShunterAuthError("token rejected", { code: "auth_denied" });
const authErrorKind: ShunterErrorKind = authError.kind;
const mismatch = new ShunterProtocolMismatchError("unsupported protocol", {
  expected: generatedProtocolMetadata,
  receivedSubprotocol: "v1.bsatn.spacetimedb",
});

const activeMessages: SubscriptionHandle<MessagesRow> = {
  queryId: 1,
  state: { status: "active", rows: [] },
  closed: Promise.resolve({ reason: "unsubscribed" }),
  unsubscribe() {},
};

async function exerciseGeneratedBindings(): Promise<void> {
  const reducerCaller: ReducerCaller = async (_name, args) => args;
  const reducerBytes: Uint8Array = await callCreateMessage(
    reducerCaller,
    new Uint8Array([1, 2, 3]),
  );

  const declaredQueryRunner: DeclaredQueryRunner = async (name) =>
    new Uint8Array([name.length]);
  const queryBytes: Uint8Array = await queryRecentMessages(declaredQueryRunner);

  const declaredViewSubscriber: DeclaredViewSubscriber = async (_name) => () => {};
  const unsubscribeView: SubscriptionUnsubscribe =
    await subscribeLiveMessageProjection(declaredViewSubscriber);
  await unsubscribeView();

  const runtimeTableSubscriber: TableSubscriber = async () => () => {};
  const runtimeBindings: RuntimeBindings<
    TableName,
    TableRows,
    ReducerName,
    ExecutableQueryName,
    ExecutableViewName
  > = {
    callReducer: reducerCaller,
    runDeclaredQuery: declaredQueryRunner,
    subscribeDeclaredView: declaredViewSubscriber,
    subscribeTable: runtimeTableSubscriber,
  };
  const unsubscribeFromBindings: SubscriptionUnsubscribe =
    await subscribeLiveMessageCount(runtimeBindings.subscribeDeclaredView);
  await unsubscribeFromBindings();

  const tableSubscriber: TableSubscriber<MessagesRow> = async (table, onRows) => {
    onRows?.([
      {
        id: 1n,
        sender: "identity",
        topic: null,
        body: table,
        sentAt: 2n,
      },
    ]);
    return () => {};
  };
  const unsubscribeTable: SubscriptionUnsubscribe = await subscribeMessages(tableSubscriber);
  await unsubscribeTable();

  void reducerBytes;
  void queryBytes;
}

void connectedState;
void runtimeProtocolMetadata;
void authErrorKind;
void mismatch;
void activeMessages;
void exerciseGeneratedBindings;
