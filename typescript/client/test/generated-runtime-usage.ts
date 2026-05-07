import {
  SHUNTER_SUBPROTOCOL_V1,
  encodeDeclaredQueryRequest,
  encodeDeclaredViewSubscriptionRequest,
  encodeReducerCallRequest,
  ShunterAuthError,
  ShunterProtocolMismatchError,
  createShunterClient,
  shunterProtocol as runtimeProtocol,
} from "../src/index";
import type {
  ConnectionState,
  EncodedDeclaredQueryRequest,
  EncodedDeclaredViewSubscriptionRequest,
  EncodedReducerCallRequest,
  ProtocolMetadata,
  RuntimeBindings,
  ShunterErrorKind,
  SubscriptionHandle,
} from "../src/index";
import {
  callCreateMessage,
  queryRecentMessages,
  reducers,
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

const client = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  protocol: generatedProtocol,
  token: async () => "token",
  webSocketFactory: () => ({
    protocol: SHUNTER_SUBPROTOCOL_V1,
    binaryType: "arraybuffer",
    addEventListener() {},
    removeEventListener() {},
    send() {},
    close() {},
  }),
});

async function exerciseGeneratedBindings(): Promise<void> {
  const generatedClientReducerCaller: ReducerCaller = client.callReducer;
  const generatedClientDeclaredQueryRunner: DeclaredQueryRunner = client.runDeclaredQuery;
  const generatedClientDeclaredViewSubscriber: DeclaredViewSubscriber = client.subscribeDeclaredView;
  const encodedRequest: EncodedReducerCallRequest<ReducerName> =
    encodeReducerCallRequest(reducers.createMessage, new Uint8Array([1, 2, 3]), {
      requestId: 9,
    });
  const encodedFrame: Uint8Array = encodedRequest.frame;
  const encodedQueryRequest: EncodedDeclaredQueryRequest<ExecutableQueryName> =
    encodeDeclaredQueryRequest("recent_messages", { requestId: 10 });
  const encodedQueryFrame: Uint8Array = encodedQueryRequest.frame;
  const encodedViewRequest: EncodedDeclaredViewSubscriptionRequest<ExecutableViewName> =
    encodeDeclaredViewSubscriptionRequest("live_message_projection", {
      requestId: 11,
      queryId: 12,
    });
  const encodedViewFrame: Uint8Array = encodedViewRequest.frame;

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
    callReducer: generatedClientReducerCaller,
    runDeclaredQuery: generatedClientDeclaredQueryRunner,
    subscribeDeclaredView: generatedClientDeclaredViewSubscriber,
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
  void encodedFrame;
  void encodedQueryFrame;
  void encodedViewFrame;
  void queryBytes;
}

void connectedState;
void runtimeProtocolMetadata;
void authErrorKind;
void mismatch;
void activeMessages;
void client;
void exerciseGeneratedBindings;
