export declare const SHUNTER_PROTOCOL_V1: 1;
export declare const SHUNTER_PROTOCOL_V2: 2;
export declare const SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION: 1;
export declare const SHUNTER_CURRENT_PROTOCOL_VERSION: 2;
export declare const SHUNTER_SUBPROTOCOL_V1: "v1.bsatn.shunter";
export declare const SHUNTER_SUBPROTOCOL_V2: "v2.bsatn.shunter";
export declare const SHUNTER_DEFAULT_SUBPROTOCOL: "v2.bsatn.shunter";
export declare const SHUNTER_SUPPORTED_SUBPROTOCOLS: readonly ["v2.bsatn.shunter", "v1.bsatn.shunter"];
export declare const SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_SINGLE: 1;
export declare const SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_SINGLE: 2;
export declare const SHUNTER_CLIENT_MESSAGE_CALL_REDUCER: 3;
export declare const SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_MULTI: 6;
export declare const SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY: 7;
export declare const SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW: 8;
export declare const SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY_WITH_PARAMETERS: 9;
export declare const SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW_WITH_PARAMETERS: 10;
export declare const SHUNTER_SERVER_MESSAGE_IDENTITY_TOKEN: 1;
export declare const SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED: 2;
export declare const SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED: 3;
export declare const SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR: 4;
export declare const SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE: 6;
export declare const SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE: 5;
export declare const SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT: 8;
export declare const SHUNTER_SERVER_MESSAGE_SUBSCRIBE_MULTI_APPLIED: 9;
export declare const SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED: 10;
export declare const SHUNTER_CALL_REDUCER_FLAGS_FULL_UPDATE: 0;
export declare const SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY: 1;
export declare const SHUNTER_MODULE_CONTRACT_FORMAT: "shunter.module_contract";
export declare const SHUNTER_MODULE_CONTRACT_VERSION_V1: 1;
export declare const SHUNTER_MIN_SUPPORTED_MODULE_CONTRACT_VERSION: 1;
export declare const SHUNTER_CURRENT_MODULE_CONTRACT_VERSION: 1;
export type ShunterSubprotocol = (typeof SHUNTER_SUPPORTED_SUBPROTOCOLS)[number];
export interface ProtocolMetadata<Subprotocol extends string = string> {
    readonly minSupportedVersion: number;
    readonly currentVersion: number;
    readonly defaultSubprotocol: Subprotocol;
    readonly supportedSubprotocols: readonly Subprotocol[];
}
export declare const shunterProtocol: {
    readonly minSupportedVersion: 1;
    readonly currentVersion: 2;
    readonly defaultSubprotocol: "v2.bsatn.shunter";
    readonly supportedSubprotocols: readonly ["v2.bsatn.shunter", "v1.bsatn.shunter"];
};
export interface ProtocolCompatibilityIssue {
    readonly code: "client_too_old" | "client_too_new" | "unsupported_default_subprotocol" | "unsupported_selected_subprotocol";
    readonly message: string;
    readonly receivedVersion?: number;
    readonly receivedSubprotocol?: string;
}
export type ProtocolCompatibilityResult = {
    readonly ok: true;
    readonly subprotocol: ShunterSubprotocol;
} | {
    readonly ok: false;
    readonly issue: ProtocolCompatibilityIssue;
};
export type ShunterErrorKind = "auth" | "contract_mismatch" | "validation" | "protocol" | "protocol_mismatch" | "transport" | "timeout" | "closed";
export interface ShunterErrorOptions {
    readonly code?: string;
    readonly details?: unknown;
    readonly cause?: unknown;
}
export declare class ShunterError extends Error {
    readonly kind: ShunterErrorKind;
    readonly code?: string;
    readonly details?: unknown;
    readonly cause?: unknown;
    constructor(kind: ShunterErrorKind, message: string, options?: ShunterErrorOptions);
}
export declare class ShunterAuthError extends ShunterError {
    constructor(message: string, options?: ShunterErrorOptions);
}
export declare class ShunterValidationError extends ShunterError {
    constructor(message: string, options?: ShunterErrorOptions);
}
export declare class ShunterProtocolError extends ShunterError {
    constructor(message: string, options?: ShunterErrorOptions);
}
export interface ShunterProtocolMismatchErrorOptions extends ShunterErrorOptions {
    readonly expected: ProtocolMetadata;
    readonly receivedVersion?: number;
    readonly receivedSubprotocol?: string;
}
export declare class ShunterProtocolMismatchError extends ShunterError {
    readonly expected: ProtocolMetadata;
    readonly receivedVersion?: number;
    readonly receivedSubprotocol?: string;
    constructor(message: string, options: ShunterProtocolMismatchErrorOptions);
}
export declare class ShunterTransportError extends ShunterError {
    constructor(message: string, options?: ShunterErrorOptions);
}
export declare class ShunterTimeoutError extends ShunterError {
    constructor(message: string, options?: ShunterErrorOptions);
}
export declare class ShunterClosedClientError extends ShunterError {
    constructor(message: string, options?: ShunterErrorOptions);
}
export declare function isShunterError(error: unknown): error is ShunterError;
export declare function toShunterError(error: unknown, kind?: ShunterErrorKind, message?: string): ShunterError;
export declare function checkProtocolCompatibility(protocol: ProtocolMetadata, selectedSubprotocol?: string): ProtocolCompatibilityResult;
export declare function assertProtocolCompatible(protocol: ProtocolMetadata, selectedSubprotocol?: string): ShunterSubprotocol;
export declare function selectShunterSubprotocol(protocol: ProtocolMetadata): ShunterSubprotocol;
export type ConnectionStatus = "idle" | "connecting" | "connected" | "reconnecting" | "closing" | "closed" | "failed";
export interface GeneratedContractMetadata<Protocol extends ProtocolMetadata = ProtocolMetadata> {
    readonly contractFormat: string;
    readonly contractVersion: number;
    readonly moduleName?: string;
    readonly moduleVersion?: string;
    readonly protocol: Protocol;
}
export interface GeneratedContractCompatibilityOptions {
    readonly protocol?: ProtocolMetadata;
    readonly selectedSubprotocol?: string;
    readonly moduleName?: string;
    readonly moduleVersion?: string;
}
export interface GeneratedContractCompatibilityIssue {
    readonly code: "unsupported_contract_format" | "generated_contract_too_new" | "generated_contract_too_old" | "protocol_metadata_mismatch" | "protocol_compatibility" | "module_name_mismatch" | "module_version_mismatch";
    readonly message: string;
    readonly receivedFormat?: string;
    readonly receivedVersion?: number;
    readonly receivedModuleName?: string;
    readonly receivedModuleVersion?: string;
    readonly protocolIssue?: ProtocolCompatibilityIssue;
}
export type GeneratedContractCompatibilityResult<Contract extends GeneratedContractMetadata = GeneratedContractMetadata> = {
    readonly ok: true;
    readonly contract: Contract;
} | {
    readonly ok: false;
    readonly contract: Contract;
    readonly issue: GeneratedContractCompatibilityIssue;
};
export interface ShunterContractMismatchErrorOptions extends ShunterErrorOptions {
    readonly contract: GeneratedContractMetadata;
    readonly issue: GeneratedContractCompatibilityIssue;
}
export declare class ShunterContractMismatchError extends ShunterError {
    readonly contract: GeneratedContractMetadata;
    readonly issue: GeneratedContractCompatibilityIssue;
    constructor(message: string, options: ShunterContractMismatchErrorOptions);
}
export declare function checkGeneratedContractCompatibility<Contract extends GeneratedContractMetadata>(contract: Contract, options?: GeneratedContractCompatibilityOptions): GeneratedContractCompatibilityResult<Contract>;
export declare function assertGeneratedContractCompatible<Contract extends GeneratedContractMetadata>(contract: Contract, options?: GeneratedContractCompatibilityOptions): Contract;
export interface ConnectionMetadata<Protocol extends ProtocolMetadata = ProtocolMetadata> {
    readonly protocol: Protocol;
    readonly subprotocol: Protocol["defaultSubprotocol"] | string;
    readonly identityToken?: string;
    readonly identity?: Uint8Array;
    readonly connectionId?: Uint8Array;
    readonly contract?: GeneratedContractMetadata;
}
export interface IdentityTokenMessage {
    readonly identity: Uint8Array;
    readonly token: string;
    readonly connectionId: Uint8Array;
}
export type ConnectionState<Protocol extends ProtocolMetadata = ProtocolMetadata> = {
    readonly status: "idle";
} | {
    readonly status: "connecting";
    readonly attempt: number;
} | {
    readonly status: "connected";
    readonly metadata: ConnectionMetadata<Protocol>;
} | {
    readonly status: "reconnecting";
    readonly attempt: number;
    readonly error?: ShunterError;
} | {
    readonly status: "closing";
} | {
    readonly status: "closed";
    readonly error?: ShunterError;
} | {
    readonly status: "failed";
    readonly error: ShunterError;
};
export interface ConnectionStateChange<Protocol extends ProtocolMetadata = ProtocolMetadata> {
    readonly previous: ConnectionState<Protocol>;
    readonly current: ConnectionState<Protocol>;
}
export type ConnectionStateListener<Protocol extends ProtocolMetadata = ProtocolMetadata> = (change: ConnectionStateChange<Protocol>) => void;
export type TokenProvider = () => string | Promise<string>;
export type TokenSource = string | TokenProvider;
export interface ReconnectOptions {
    readonly enabled?: boolean;
    readonly maxAttempts?: number;
    readonly initialDelayMs?: number;
    readonly maxDelayMs?: number;
    readonly backoffMultiplier?: number;
    readonly resubscribe?: boolean;
}
export interface RuntimeClientOptions<Protocol extends ProtocolMetadata = ProtocolMetadata> {
    readonly url: string;
    readonly protocol: Protocol;
    readonly contract?: GeneratedContractMetadata<Protocol>;
    readonly token?: TokenSource;
    readonly signal?: AbortSignal;
    readonly webSocketFactory?: WebSocketFactory;
    readonly onStateChange?: ConnectionStateListener<Protocol>;
    readonly reconnect?: ReconnectOptions | false;
}
export interface WebSocketLike {
    readonly protocol: string;
    binaryType?: BinaryType;
    addEventListener(type: "open", listener: (event: Event) => void): void;
    addEventListener(type: "message", listener: (event: MessageEvent) => void): void;
    addEventListener(type: "close", listener: (event: CloseEvent) => void): void;
    addEventListener(type: "error", listener: (event: Event) => void): void;
    removeEventListener(type: "open", listener: (event: Event) => void): void;
    removeEventListener(type: "message", listener: (event: MessageEvent) => void): void;
    removeEventListener(type: "close", listener: (event: CloseEvent) => void): void;
    removeEventListener(type: "error", listener: (event: Event) => void): void;
    send(data: WebSocketSendData): void;
    close(code?: number, reason?: string): void;
}
export type WebSocketSendData = string | ArrayBufferLike | ArrayBufferView | Blob;
export type WebSocketFactory = (url: string, protocols: readonly ShunterSubprotocol[]) => WebSocketLike;
export interface ShunterClient<Protocol extends ProtocolMetadata = ProtocolMetadata> {
    readonly state: ConnectionState<Protocol>;
    connect(): Promise<ConnectionMetadata<Protocol>>;
    callReducer: ReducerCaller<string, Uint8Array, Uint8Array>;
    runDeclaredQuery: DeclaredQueryRunner<string, Uint8Array>;
    subscribeDeclaredView: DeclaredViewSubscriber<string> & DeclaredViewHandleSubscriber<string>;
    subscribeTable: RawTableSubscriber & RawTableHandleSubscriber & DecodedTableHandleSubscriber;
    close(code?: number, reason?: string): Promise<void>;
    dispose(): Promise<void>;
    onStateChange(listener: ConnectionStateListener<Protocol>): () => void;
}
export declare function createShunterClient<Protocol extends ProtocolMetadata>(options: RuntimeClientOptions<Protocol>): ShunterClient<Protocol>;
export declare function decodeIdentityTokenFrame(data: unknown): IdentityTokenMessage;
export type TransactionUpdateStatus = {
    readonly status: "committed";
    readonly updates: readonly RawSubscriptionUpdate[];
} | {
    readonly status: "failed";
    readonly error: string;
};
export interface RawSubscriptionUpdate {
    readonly queryId: QueryID;
    readonly tableName: string;
    readonly inserts: Uint8Array;
    readonly deletes: Uint8Array;
    readonly insertRowBytes?: readonly Uint8Array[];
    readonly deleteRowBytes?: readonly Uint8Array[];
}
export type RawSubscriptionUpdateCallback = (update: RawSubscriptionUpdate) => void;
export interface ReducerCallInfo<Name extends string = string> {
    readonly name: Name;
    readonly reducerId: number;
    readonly args: Uint8Array;
    readonly requestId: RequestID;
}
export interface TransactionUpdateMessage<Name extends string = string> {
    readonly status: TransactionUpdateStatus;
    readonly timestamp: bigint;
    readonly callerIdentity: Uint8Array;
    readonly callerConnectionId: Uint8Array;
    readonly reducerCall: ReducerCallInfo<Name>;
    readonly totalHostExecutionDuration: bigint;
    readonly rawFrame: Uint8Array;
}
export interface TransactionUpdateLightMessage {
    readonly requestId: RequestID;
    readonly updates: readonly RawSubscriptionUpdate[];
    readonly rawFrame: Uint8Array;
}
export declare function decodeTransactionUpdateFrame(data: unknown): TransactionUpdateMessage;
export declare function decodeTransactionUpdateLightFrame(data: unknown): TransactionUpdateLightMessage;
export interface OneOffQueryTable {
    readonly tableName: string;
    readonly rows: Uint8Array;
    readonly rowBytes: readonly Uint8Array[];
}
export interface OneOffQueryResponseMessage {
    readonly messageId: Uint8Array;
    readonly error?: string;
    readonly tables: readonly OneOffQueryTable[];
    readonly totalHostExecutionDuration: bigint;
    readonly rawFrame: Uint8Array;
}
export type RawDeclaredQueryTable = OneOffQueryTable;
export interface RawDeclaredQueryResult<Name extends string = string> {
    readonly name: Name;
    readonly messageId: Uint8Array;
    readonly tables: readonly RawDeclaredQueryTable[];
    readonly totalHostExecutionDuration: bigint;
    readonly rawFrame: Uint8Array;
}
export interface DecodedDeclaredQueryTable<Table extends string = string, Row = unknown> {
    readonly tableName: Table;
    readonly rows: readonly Row[];
    readonly rawRows: Uint8Array;
    readonly rowBytes: readonly Uint8Array[];
}
export type DecodedDeclaredQueryTableFor<RowsByName extends object> = {
    readonly [Name in keyof RowsByName & string]: DecodedDeclaredQueryTable<Name, RowsByName[Name]>;
}[keyof RowsByName & string];
export interface DecodedDeclaredQueryResult<Name extends string = string, RowsByName extends object = Record<string, unknown>> {
    readonly name: Name;
    readonly messageId: Uint8Array;
    readonly tables: readonly DecodedDeclaredQueryTableFor<RowsByName>[];
    readonly totalHostExecutionDuration: bigint;
    readonly rawFrame: Uint8Array;
}
export type DeclaredQueryRowDecoder<Row = unknown> = (tableName: string, row: Uint8Array) => Row;
export interface DeclaredQueryDecodeOptions<RowsByName extends object = Record<string, unknown>> {
    readonly tableDecoders?: TableRowDecoders<RowsByName>;
    readonly decodeRow?: DeclaredQueryRowDecoder<RowsByName[keyof RowsByName & string]>;
}
export declare function decodeOneOffQueryResponseFrame(data: unknown): OneOffQueryResponseMessage;
export declare function decodeRawDeclaredQueryResult<Name extends string>(name: Name, data: unknown): RawDeclaredQueryResult<Name>;
export declare function decodeDeclaredQueryResult<Name extends string, RowsByName extends object = Record<string, unknown>>(name: Name, data: unknown, options: DeclaredQueryDecodeOptions<RowsByName>): DecodedDeclaredQueryResult<Name, RowsByName>;
export interface RawRowList {
    readonly rows: readonly Uint8Array[];
    readonly rawBytes: Uint8Array;
}
export type BsatnValueKind = "bool" | "int8" | "uint8" | "int16" | "uint16" | "int32" | "uint32" | "int64" | "uint64" | "float32" | "float64" | "string" | "bytes" | "int128" | "uint128" | "int256" | "uint256" | "timestamp" | "arrayString" | "uuid" | "duration" | "json";
export interface BsatnColumn {
    readonly name: string;
    readonly kind: BsatnValueKind;
    readonly nullable?: boolean;
}
export declare function decodeRowList(data: unknown): RawRowList;
export declare function decodeBsatnProduct<Row>(data: unknown, columns: readonly BsatnColumn[], buildRow: (values: readonly unknown[]) => Row): Row;
export declare function encodeBsatnProduct(values: readonly unknown[], columns: readonly BsatnColumn[]): Uint8Array;
export interface SubscribeSingleAppliedMessage {
    readonly requestId: RequestID;
    readonly totalHostExecutionDurationMicros: bigint;
    readonly queryId: QueryID;
    readonly tableName: string;
    readonly rows: Uint8Array;
    readonly rowBytes: readonly Uint8Array[];
    readonly rawFrame: Uint8Array;
}
export interface UnsubscribeSingleAppliedMessage {
    readonly requestId: RequestID;
    readonly totalHostExecutionDurationMicros: bigint;
    readonly queryId: QueryID;
    readonly hasRows: boolean;
    readonly rows?: Uint8Array;
    readonly rowBytes?: readonly Uint8Array[];
    readonly rawFrame: Uint8Array;
}
export interface SubscriptionSetAppliedMessage {
    readonly requestId: RequestID;
    readonly totalHostExecutionDurationMicros: bigint;
    readonly queryId: QueryID;
    readonly updates: readonly RawSubscriptionUpdate[];
    readonly rawFrame: Uint8Array;
}
export interface SubscriptionErrorMessage {
    readonly totalHostExecutionDurationMicros: bigint;
    readonly requestId?: RequestID;
    readonly queryId?: QueryID;
    readonly tableId?: number;
    readonly error: string;
    readonly rawFrame: Uint8Array;
}
export declare function decodeSubscribeSingleAppliedFrame(data: unknown): SubscribeSingleAppliedMessage;
export declare function decodeUnsubscribeSingleAppliedFrame(data: unknown): UnsubscribeSingleAppliedMessage;
export declare function decodeSubscribeMultiAppliedFrame(data: unknown): SubscriptionSetAppliedMessage;
export declare function decodeUnsubscribeMultiAppliedFrame(data: unknown): SubscriptionSetAppliedMessage;
export declare function decodeSubscriptionErrorFrame(data: unknown): SubscriptionErrorMessage;
export type RequestID = number;
export type QueryID = number;
export type TransactionID = number | bigint | string;
export interface ReducerCallOptions {
    readonly requestId?: RequestID;
    readonly noSuccessNotify?: boolean;
    readonly signal?: AbortSignal;
}
export type ReducerArgEncoder<Args = unknown> = (args: Args) => Uint8Array;
export interface ReducerArgEncodingOptions<Args = unknown> {
    readonly encodeArgs?: ReducerArgEncoder<Args>;
}
export interface EncodedReducerCallOptions<Args = unknown> extends ReducerCallOptions, ReducerArgEncodingOptions<Args> {
}
export type ReducerCallFlags = typeof SHUNTER_CALL_REDUCER_FLAGS_FULL_UPDATE | typeof SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY;
export interface EncodedReducerCallRequest<Name extends string = string> {
    readonly name: Name;
    readonly args: Uint8Array;
    readonly requestId: RequestID;
    readonly flags: ReducerCallFlags;
    readonly frame: Uint8Array;
}
export interface ReducerCallResult<Name extends string = string, Result = Uint8Array> {
    readonly name: Name;
    readonly requestId: RequestID;
    readonly status: "committed" | "failed";
    readonly txId?: TransactionID;
    readonly value?: Result;
    readonly rawResult?: Uint8Array;
    readonly error?: ShunterError;
}
export interface ReducerCallResultOptions<Result = Uint8Array> {
    readonly requestId?: RequestID;
    readonly decodeResult?: (update: TransactionUpdateMessage) => Result;
}
export interface ReducerCallResultRequestOptions<Result = Uint8Array> extends ReducerCallResultOptions<Result> {
    readonly signal?: AbortSignal;
}
export interface EncodedReducerCallResultOptions<Args = unknown, Result = Uint8Array> extends ReducerCallResultRequestOptions<Result>, ReducerArgEncodingOptions<Args> {
}
export type ReducerCaller<Name extends string = string, Args = Uint8Array, Result = Uint8Array> = (name: Name, args: Args, options?: ReducerCallOptions) => Promise<Result>;
export declare function encodeReducerArgs(args: Uint8Array): Uint8Array;
export declare function encodeReducerArgs<Args>(args: Args, encodeArgs: ReducerArgEncoder<Args>): Uint8Array;
export declare function reducerCallOptions<Args>(options: EncodedReducerCallOptions<Args>): ReducerCallOptions;
export declare function reducerCallResultRequestOptions<Args, Result>(options: EncodedReducerCallResultOptions<Args, Result>): ReducerCallResultRequestOptions<Result>;
export declare function callReducerWithEncodedArgs<Name extends string, Args>(callReducer: ReducerCaller<Name, Uint8Array, Uint8Array>, name: Name, args: Args, options?: EncodedReducerCallOptions<Args>): Promise<Uint8Array>;
export declare function callReducerWithResult<Name extends string, Result = Uint8Array>(callReducer: ReducerCaller<Name, Uint8Array, Uint8Array>, name: Name, args: Uint8Array, options?: ReducerCallResultRequestOptions<Result>): Promise<ReducerCallResult<Name, Result>>;
export declare function callReducerWithEncodedArgsResult<Name extends string, Args, Result = Uint8Array>(callReducer: ReducerCaller<Name, Uint8Array, Uint8Array>, name: Name, args: Args, options?: EncodedReducerCallResultOptions<Args, Result>): Promise<ReducerCallResult<Name, Result>>;
export declare function encodeReducerCallRequest<Name extends string>(name: Name, args: Uint8Array, options?: ReducerCallOptions): EncodedReducerCallRequest<Name>;
export declare function decodeReducerCallResult<Name extends string, Result = Uint8Array>(name: Name, data: unknown, options?: ReducerCallResultOptions<Result>): ReducerCallResult<Name, Result>;
export interface RawQueryOptions {
    readonly requestId?: RequestID;
    readonly signal?: AbortSignal;
}
export type QueryRunner<Result = Uint8Array> = (sql: string, options?: RawQueryOptions) => Promise<Result>;
export interface DeclaredQueryOptions {
    readonly requestId?: RequestID;
    readonly messageId?: Uint8Array;
    readonly signal?: AbortSignal;
    readonly params?: Uint8Array;
}
export interface EncodedDeclaredQueryRequest<Name extends string = string> {
    readonly name: Name;
    readonly requestId?: RequestID;
    readonly messageId: Uint8Array;
    readonly params?: Uint8Array;
    readonly frame: Uint8Array;
}
export interface DeclaredQueryResult<Name extends string = string, Rows = Uint8Array> {
    readonly name: Name;
    readonly requestId: RequestID;
    readonly rows: Rows;
    readonly rawRows?: Uint8Array;
}
export type DeclaredQueryRunner<Name extends string = string, Result = Uint8Array> = (name: Name, options?: DeclaredQueryOptions) => Promise<Result>;
export declare function encodeDeclaredQueryRequest<Name extends string>(name: Name, options?: DeclaredQueryOptions): EncodedDeclaredQueryRequest<Name>;
export interface SubscriptionClosed {
    readonly reason: "unsubscribed" | "closed" | "error";
    readonly error?: ShunterError;
}
export type SubscriptionState<Row = unknown> = {
    readonly status: "subscribing";
} | {
    readonly status: "active";
    readonly rows: readonly Row[];
} | {
    readonly status: "unsubscribing";
    readonly rows: readonly Row[];
} | {
    readonly status: "closed";
    readonly error?: ShunterError;
};
export interface SubscriptionUpdate<Row = unknown> {
    readonly queryId: QueryID;
    readonly tableName: string;
    readonly inserts: readonly Row[];
    readonly deletes: readonly Row[];
}
export type RowDecoder<Row = unknown> = (row: Uint8Array) => Row;
export type TableRowDecoder<Row = unknown> = RowDecoder<Row>;
export type TableRowDecoders<RowsByName extends object = Record<string, unknown>> = {
    readonly [Name in keyof RowsByName]?: TableRowDecoder<RowsByName[Name]>;
};
export interface SubscriptionHandle<Row = unknown> {
    readonly queryId?: QueryID;
    readonly state: SubscriptionState<Row>;
    readonly closed: Promise<SubscriptionClosed>;
    unsubscribe(): void | Promise<void>;
}
export interface ManagedSubscriptionHandle<Row = unknown> extends SubscriptionHandle<Row> {
    replaceRows(rows: readonly Row[]): void;
    close(error?: ShunterError): void;
}
export interface SubscriptionHandleOptions<Row = unknown> {
    readonly queryId?: QueryID;
    readonly initialRows?: readonly Row[];
    readonly unsubscribe?: () => void | Promise<void>;
    readonly onStateChange?: (state: SubscriptionState<Row>) => void;
}
export type SubscriptionUnsubscribe = () => void | Promise<void>;
export interface SubscriptionHandleReturnOptions {
    readonly returnHandle: true;
}
export declare function createSubscriptionHandle<Row = unknown>(options?: SubscriptionHandleOptions<Row>): ManagedSubscriptionHandle<Row>;
export interface DeclaredViewSubscriptionOptions<Row = unknown> {
    readonly requestId?: RequestID;
    readonly queryId?: QueryID;
    readonly signal?: AbortSignal;
    readonly returnHandle?: boolean;
    readonly decodeRow?: RowDecoder<Row>;
    readonly onInitialRows?: (rows: readonly Row[]) => void;
    readonly onUpdate?: (update: SubscriptionUpdate<Row>) => void;
    readonly onRawUpdate?: RawSubscriptionUpdateCallback;
    readonly params?: Uint8Array;
}
export interface EncodedSubscribeSingleRequest {
    readonly queryString: string;
    readonly requestId: RequestID;
    readonly queryId: QueryID;
    readonly frame: Uint8Array;
}
export interface EncodedTableSubscriptionRequest<Table extends string = string> extends EncodedSubscribeSingleRequest {
    readonly table: Table;
}
export interface EncodedDeclaredViewSubscriptionRequest<Name extends string = string> {
    readonly name: Name;
    readonly requestId: RequestID;
    readonly queryId: QueryID;
    readonly params?: Uint8Array;
    readonly frame: Uint8Array;
}
export interface EncodedSubscriptionUnsubscribeRequest {
    readonly requestId: RequestID;
    readonly queryId: QueryID;
    readonly frame: Uint8Array;
}
export interface EncodedUnsubscribeSingleRequest extends EncodedSubscriptionUnsubscribeRequest {
}
export interface EncodedUnsubscribeMultiRequest {
    readonly requestId: RequestID;
    readonly queryId: QueryID;
    readonly frame: Uint8Array;
}
export type DeclaredViewSubscriber<Name extends string = string> = <Row = Uint8Array>(name: Name, options?: DeclaredViewSubscriptionOptions<Row>) => Promise<SubscriptionUnsubscribe>;
export type DeclaredViewHandleSubscriber<Name extends string = string> = <Row = Uint8Array>(name: Name, options: DeclaredViewSubscriptionOptions<Row> & SubscriptionHandleReturnOptions) => Promise<SubscriptionHandle<Row>>;
export declare function encodeSubscribeSingleRequest<Row = unknown>(queryString: string, options?: TableSubscriptionOptions<Row>): EncodedSubscribeSingleRequest;
export declare function encodeTableSubscriptionRequest<Table extends string, Row = unknown>(table: Table, options?: TableSubscriptionOptions<Row>): EncodedTableSubscriptionRequest<Table>;
export declare function encodeDeclaredViewSubscriptionRequest<Name extends string>(name: Name, options?: DeclaredViewSubscriptionOptions): EncodedDeclaredViewSubscriptionRequest<Name>;
export declare function encodeUnsubscribeSingleRequest(queryId: QueryID, options?: {
    readonly requestId?: RequestID;
}): EncodedUnsubscribeSingleRequest;
export declare function encodeUnsubscribeMultiRequest(queryId: QueryID, options?: {
    readonly requestId?: RequestID;
}): EncodedUnsubscribeMultiRequest;
export interface TableSubscriptionOptions<Row = unknown> {
    readonly requestId?: RequestID;
    readonly queryId?: QueryID;
    readonly signal?: AbortSignal;
    readonly returnHandle?: boolean;
    readonly decodeRow?: RowDecoder<Row>;
    readonly onInitialRows?: (rows: readonly Row[]) => void;
    readonly onRawRows?: (message: SubscribeSingleAppliedMessage) => void;
    readonly onRawUpdate?: RawSubscriptionUpdateCallback;
    readonly onUpdate?: (update: SubscriptionUpdate<Row>) => void;
}
export type TableSubscriber<Name extends string = string, RowsByName extends Record<Name, unknown> = Record<Name, unknown>, Row = never> = <Table extends Name>(table: Table, onRows?: (rows: ([Row] extends [never] ? RowsByName[Table] : Row)[]) => void, options?: TableSubscriptionOptions<[Row] extends [never] ? RowsByName[Table] : Row>) => Promise<SubscriptionUnsubscribe>;
export type TableHandleSubscriber<Name extends string = string> = <Table extends Name>(table: Table, onRows: ((rows: Uint8Array[]) => void) | undefined, options: TableSubscriptionOptions<Uint8Array> & SubscriptionHandleReturnOptions) => Promise<SubscriptionHandle<Uint8Array>>;
export type DecodedTableHandleSubscriber = <Table extends string, Row>(table: Table, onRows: ((rows: Row[]) => void) | undefined, options: TableSubscriptionOptions<Row> & SubscriptionHandleReturnOptions & {
    readonly decodeRow: RowDecoder<Row>;
}) => Promise<SubscriptionHandle<Row>>;
export type RawTableSubscriber = <Table extends string, Row = unknown>(table: Table, onRows?: (rows: Row[]) => void, options?: TableSubscriptionOptions<Row>) => Promise<SubscriptionUnsubscribe>;
export type RawTableHandleSubscriber = <Table extends string>(table: Table, onRows: ((rows: Uint8Array[]) => void) | undefined, options: TableSubscriptionOptions<Uint8Array> & SubscriptionHandleReturnOptions) => Promise<SubscriptionHandle<Uint8Array>>;
export type ViewSubscriber = (sql: string, options?: DeclaredViewSubscriptionOptions) => Promise<SubscriptionUnsubscribe>;
export interface RuntimeBindings<TableName extends string = string, RowsByName extends Record<TableName, unknown> = Record<TableName, unknown>, ReducerName extends string = string, ExecutableQueryName extends string = string, ExecutableViewName extends string = string> {
    readonly callReducer: ReducerCaller<ReducerName, Uint8Array, Uint8Array>;
    readonly runDeclaredQuery: DeclaredQueryRunner<ExecutableQueryName, Uint8Array>;
    readonly subscribeDeclaredView: DeclaredViewSubscriber<ExecutableViewName>;
    readonly subscribeTable: TableSubscriber<TableName, RowsByName>;
}
//# sourceMappingURL=index.d.ts.map