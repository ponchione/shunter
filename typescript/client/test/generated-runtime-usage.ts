import {
  SHUNTER_SUBPROTOCOL_V1,
  encodeDeclaredQueryRequest,
  encodeDeclaredViewSubscriptionRequest,
  encodeReducerCallRequest,
  encodeSubscribeSingleRequest,
  encodeTableSubscriptionRequest,
  decodeRawDeclaredQueryResult,
  decodeReducerCallResult,
  decodeRowList,
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
  EncodedSubscribeSingleRequest,
  EncodedTableSubscriptionRequest,
  ProtocolMetadata,
  RawRowList,
  RawDeclaredQueryResult,
  RawDeclaredQueryTable,
  RawSubscriptionUpdate,
  ReducerCallResult,
  DeclaredViewHandleSubscriber,
  RuntimeBindings,
  ShunterErrorKind,
  SubscribeSingleAppliedMessage,
  SubscriptionHandle,
  TableHandleSubscriber,
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
  const generatedClientDeclaredViewHandleSubscriber: DeclaredViewHandleSubscriber<ExecutableViewName> =
    client.subscribeDeclaredView;
  const generatedClientTableSubscriber: TableSubscriber = client.subscribeTable;
  const generatedClientTableHandleSubscriber: TableHandleSubscriber<TableName> =
    client.subscribeTable;
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
  const encodedSubscribeSingle: EncodedSubscribeSingleRequest =
    encodeSubscribeSingleRequest("SELECT * FROM messages", {
      requestId: 13,
      queryId: 14,
    });
  const encodedSubscribeSingleFrame: Uint8Array = encodedSubscribeSingle.frame;
  const encodedTableRequest: EncodedTableSubscriptionRequest<TableName> =
    encodeTableSubscriptionRequest("messages", {
      requestId: 15,
      queryId: 16,
    });
  const encodedTableFrame: Uint8Array = encodedTableRequest.frame;
  const rawUpdateHandler = (update: RawSubscriptionUpdate): void => {
    const rawInserts: Uint8Array = update.inserts;
    const insertRowBytes: readonly Uint8Array[] | undefined = update.insertRowBytes;
    const deleteRowBytes: readonly Uint8Array[] | undefined = update.deleteRowBytes;
    const insertedRows: RawRowList = decodeRowList(rawInserts);
    const insertedRowCount: number = insertedRows.rows.length;
    void insertedRowCount;
    void insertRowBytes;
    void deleteRowBytes;
    void rawInserts;
  };
  const rawRowsHandler = (message: SubscribeSingleAppliedMessage): void => {
    const rawRows: Uint8Array = message.rows;
    const rowBytes: readonly Uint8Array[] = message.rowBytes;
    const decodedRows: RawRowList = decodeRowList(rawRows);
    void rowBytes;
    void decodedRows;
    void rawRows;
  };

  const reducerCaller: ReducerCaller = async (_name, args) => args;
  const reducerBytes: Uint8Array = await callCreateMessage(
    reducerCaller,
    new Uint8Array([1, 2, 3]),
  );
  const reducerResult: ReducerCallResult<ReducerName> = {
    name: reducers.createMessage,
    requestId: 1,
    status: "committed",
    value: new Uint8Array([1]),
    rawResult: new Uint8Array([1]),
  };
  const reducerResultDecoder: typeof decodeReducerCallResult = decodeReducerCallResult;

  const declaredQueryRunner: DeclaredQueryRunner = async (name) =>
    new Uint8Array([name.length]);
  const queryBytes: Uint8Array = await queryRecentMessages(declaredQueryRunner);
  const rawDeclaredQueryTable: RawDeclaredQueryTable = {
    tableName: "messages",
    rows: new Uint8Array([0]),
    rowBytes: [new Uint8Array([0])],
  };
  const rawDeclaredQueryResult: RawDeclaredQueryResult<ExecutableQueryName> = {
    name: "recent_messages",
    messageId: new Uint8Array([1]),
    tables: [rawDeclaredQueryTable],
    totalHostExecutionDuration: 0n,
    rawFrame: new Uint8Array([0]),
  };
  const rawDeclaredQueryDecoder: typeof decodeRawDeclaredQueryResult = decodeRawDeclaredQueryResult;

  const declaredViewSubscriber: DeclaredViewSubscriber = async (_name) => () => {};
  const unsubscribeView: SubscriptionUnsubscribe =
    await subscribeLiveMessageProjection(declaredViewSubscriber);
  await unsubscribeView();

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
    subscribeTable: generatedClientTableSubscriber,
  };
  const unsubscribeFromBindings: SubscriptionUnsubscribe =
    await subscribeLiveMessageCount(runtimeBindings.subscribeDeclaredView);
  await unsubscribeFromBindings();
  const unsubscribeRawDeclaredView: SubscriptionUnsubscribe =
    await runtimeBindings.subscribeDeclaredView("live_message_projection", {
      onRawUpdate: rawUpdateHandler,
    });
  await unsubscribeRawDeclaredView();
  const unsubscribeRawTable: SubscriptionUnsubscribe =
    await runtimeBindings.subscribeTable("messages", undefined, {
      onRawRows: rawRowsHandler,
      onRawUpdate: rawUpdateHandler,
    });
  await unsubscribeRawTable();
  const declaredViewHandle: SubscriptionHandle<Uint8Array> =
    await generatedClientDeclaredViewHandleSubscriber("live_message_projection", {
      returnHandle: true,
    });
  await declaredViewHandle.unsubscribe();
  const tableHandleFromClient: SubscriptionHandle<Uint8Array> =
    await generatedClientTableHandleSubscriber("messages", undefined, {
      returnHandle: true,
    });
  await tableHandleFromClient.unsubscribe();

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
  void reducerResult;
  void reducerResultDecoder;
  void encodedFrame;
  void encodedQueryFrame;
  void encodedViewFrame;
  void encodedSubscribeSingleFrame;
  void encodedTableFrame;
  void queryBytes;
  void rawDeclaredQueryResult;
  void rawDeclaredQueryDecoder;
}

void connectedState;
void runtimeProtocolMetadata;
void authErrorKind;
void mismatch;
void activeMessages;
void client;
void exerciseGeneratedBindings;
