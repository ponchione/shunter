import {
  SHUNTER_SUBPROTOCOL_V1,
  callReducerWithEncodedArgs,
  callReducerWithEncodedArgsResult,
  decodeDeclaredQueryResult,
  encodeReducerArgs,
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
  EncodedReducerCallOptions,
  EncodedReducerCallResultOptions,
  ReducerArgEncoder,
  ReducerCallResult,
  DeclaredViewHandleSubscriber,
  DecodedDeclaredQueryResult,
  DecodedTableHandleSubscriber,
  DeclaredQueryDecodeOptions,
  DeclaredQueryRowDecoder,
  RowDecoder,
  RuntimeBindings,
  ShunterErrorKind,
  SubscribeSingleAppliedMessage,
  SubscriptionHandle,
  SubscriptionUpdate,
  TableHandleSubscriber,
  TableRowDecoder,
  TableRowDecoders,
} from "../src/index";
import {
  callCreateMessage,
  callCreateMessageResult,
  decodeMessagesRow,
  queryRecentMessages,
  queryRecentMessagesResult,
  queries,
  reducers,
  shunterProtocol as generatedProtocol,
  subscribeLiveMessageCount,
  subscribeLiveMessageProjection,
  subscribeMessages,
  tableRowDecoders as generatedTableRowDecodersValue,
} from "../../../codegen/testdata/v1_module_contract";
import type {
  DeclaredQueryRunner,
  DeclaredQueryDecodeOptions as GeneratedDeclaredQueryDecodeOptions,
  DeclaredViewSubscriber,
  DecodedDeclaredQueryResult as GeneratedDecodedDeclaredQueryResult,
  ExecutableQueryName,
  ExecutableViewName,
  MessagesRow,
  ReducerCaller,
  ReducerCallResultOptions,
  ReducerCallResult as GeneratedReducerCallResult,
  ReducerName,
  RawDeclaredQueryResult as GeneratedRawDeclaredQueryResult,
  SubscriptionUnsubscribe,
  TableName,
  TableRowDecoder as GeneratedTableRowDecoder,
  TableRowDecoders as GeneratedTableRowDecoders,
  TableRows,
  TableSubscriber,
  TableSubscriptionOptions as GeneratedTableSubscriptionOptions,
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
  const generatedClientDecodedTableHandleSubscriber: DecodedTableHandleSubscriber =
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
  const messageRowDecoder: TableRowDecoder<MessagesRow> = (_row) => ({
    id: 1n,
    sender: "identity",
    topic: null,
    body: "decoded",
    sentAt: 2n,
  });
  const rowDecoder: RowDecoder<MessagesRow> = messageRowDecoder;
  const tableRowDecoders: TableRowDecoders<TableRows> = {
    messages: messageRowDecoder,
  };
  const generatedMessageRowDecoder: GeneratedTableRowDecoder<"messages"> = decodeMessagesRow;
  const generatedMessageRowDecoderAlias: GeneratedTableRowDecoder<"messages"> =
    generatedTableRowDecodersValue.messages;
  const generatedTableRowDecoders: GeneratedTableRowDecoders = {
    messages: generatedMessageRowDecoder,
  };
  const exportedGeneratedTableRowDecoders: GeneratedTableRowDecoders =
    generatedTableRowDecodersValue;
  const generatedTableSubscriptionOptions: GeneratedTableSubscriptionOptions<MessagesRow> = {
    decodeRow: generatedMessageRowDecoder,
  };
  const declaredQueryRowDecoder: DeclaredQueryRowDecoder<MessagesRow> = (_tableName, row) =>
    messageRowDecoder(row);
  const declaredQueryDecodeOptions: DeclaredQueryDecodeOptions<TableRows> = {
    tableDecoders: tableRowDecoders,
    decodeRow: declaredQueryRowDecoder,
  };
  const generatedDeclaredQueryDecodeOptions: GeneratedDeclaredQueryDecodeOptions<TableRows> = {
    tableDecoders: generatedTableRowDecoders,
    decodeRow: declaredQueryRowDecoder,
  };
  const decodedUpdateHandler = (update: SubscriptionUpdate<MessagesRow>): void => {
    const insertedRows: readonly MessagesRow[] = update.inserts;
    const deletedRows: readonly MessagesRow[] = update.deletes;
    void insertedRows;
    void deletedRows;
  };

  const reducerCaller: ReducerCaller = async (_name, args) => args;
  const createMessageArgEncoder: ReducerArgEncoder<{ body: string }> = (args) =>
    new TextEncoder().encode(args.body);
  const encodedCreateMessageArgs: Uint8Array = encodeReducerArgs(
    { body: "hello" },
    createMessageArgEncoder,
  );
  const encodedReducerOptions: EncodedReducerCallOptions<{ body: string }> = {
    encodeArgs: createMessageArgEncoder,
    noSuccessNotify: true,
  };
  const encodedReducerResultOptions: EncodedReducerCallResultOptions<{ body: string }> = {
    encodeArgs: createMessageArgEncoder,
    requestId: 1,
  };
  const typedReducerBytes: Uint8Array = await callReducerWithEncodedArgs(
    reducerCaller,
    reducers.createMessage,
    { body: "hello" },
    encodedReducerOptions,
  );
  const typedReducerEnvelope: ReducerCallResult<typeof reducers.createMessage> =
    await callReducerWithEncodedArgsResult(
      reducerCaller,
      reducers.createMessage,
      { body: "hello" },
      encodedReducerResultOptions,
    );
  const reducerBytes: Uint8Array = await callCreateMessage(
    reducerCaller,
    new Uint8Array([1, 2, 3]),
  );
  const reducerResultOptions: ReducerCallResultOptions = { requestId: 1 };
  const generatedReducerResultPromise: Promise<GeneratedReducerCallResult<typeof reducers.createMessage>> =
    callCreateMessageResult(reducerCaller, new Uint8Array([1, 2, 3]), reducerResultOptions);
  const reducerResult: ReducerCallResult<ReducerName> = {
    name: reducers.createMessage,
    requestId: 1,
    status: "committed",
    value: new Uint8Array([1]),
    rawResult: new Uint8Array([1]),
  };
  const generatedReducerResult: GeneratedReducerCallResult<typeof reducers.createMessage> =
    reducerResult;
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
  const generatedRawDeclaredQueryResult: GeneratedRawDeclaredQueryResult<typeof queries.recentMessages> =
    rawDeclaredQueryResult;
  const rawDeclaredQueryDecoder: typeof decodeRawDeclaredQueryResult = decodeRawDeclaredQueryResult;
  const decodedDeclaredQueryResult: DecodedDeclaredQueryResult<ExecutableQueryName, TableRows> = {
    name: "recent_messages",
    messageId: new Uint8Array([1]),
    tables: [{
      tableName: "messages",
      rows: [messageRowDecoder(new Uint8Array([0]))],
      rawRows: new Uint8Array([0]),
      rowBytes: [new Uint8Array([0])],
    }],
    totalHostExecutionDuration: 0n,
    rawFrame: new Uint8Array([0]),
  };
  const generatedDecodedDeclaredQueryResult: GeneratedDecodedDeclaredQueryResult<typeof queries.recentMessages, TableRows> =
    decodedDeclaredQueryResult;
  const decodedDeclaredQueryDecoder: typeof decodeDeclaredQueryResult = decodeDeclaredQueryResult;
  const generatedDecodedDeclaredQueryDecoder: typeof queryRecentMessagesResult = queryRecentMessagesResult;

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
  const unsubscribeDecodedTable: SubscriptionUnsubscribe =
    await runtimeBindings.subscribeTable("messages", (rows) => {
      const firstBody: string | undefined = rows[0]?.body;
      void firstBody;
    }, {
      decodeRow: rowDecoder,
      onInitialRows: (rows) => {
        const firstSender: string | undefined = rows[0]?.sender;
        void firstSender;
      },
      onUpdate: decodedUpdateHandler,
    });
  await unsubscribeDecodedTable();
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
  const decodedTableHandleFromClient: SubscriptionHandle<MessagesRow> =
    await generatedClientDecodedTableHandleSubscriber("messages", undefined, {
      returnHandle: true,
      decodeRow: messageRowDecoder,
    });
  await decodedTableHandleFromClient.unsubscribe();

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
  const unsubscribeGeneratedDecodedTable: SubscriptionUnsubscribe = await subscribeMessages(
    tableSubscriber,
    (rows) => {
      const firstBody: string | undefined = rows[0]?.body;
      void firstBody;
    },
    generatedTableSubscriptionOptions,
  );
  await unsubscribeGeneratedDecodedTable();

  void reducerBytes;
  void encodedCreateMessageArgs;
  void typedReducerBytes;
  void typedReducerEnvelope;
  void generatedReducerResultPromise;
  void reducerResult;
  void generatedReducerResult;
  void reducerResultDecoder;
  void encodedFrame;
  void encodedQueryFrame;
  void encodedViewFrame;
  void encodedSubscribeSingleFrame;
  void encodedTableFrame;
  void queryBytes;
  void rawDeclaredQueryResult;
  void generatedRawDeclaredQueryResult;
  void rawDeclaredQueryDecoder;
  void decodedDeclaredQueryResult;
  void generatedDecodedDeclaredQueryResult;
  void decodedDeclaredQueryDecoder;
  void generatedDecodedDeclaredQueryDecoder;
  void generatedDeclaredQueryDecodeOptions;
  void declaredQueryDecodeOptions;
  void tableRowDecoders;
  void generatedTableRowDecoders;
  void generatedMessageRowDecoderAlias;
  void exportedGeneratedTableRowDecoders;
}

void connectedState;
void runtimeProtocolMetadata;
void authErrorKind;
void mismatch;
void activeMessages;
void client;
void exerciseGeneratedBindings;
