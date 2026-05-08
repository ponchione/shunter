export const SHUNTER_PROTOCOL_V1 = 1 as const;
export const SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_CURRENT_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_SUBPROTOCOL_V1 = "v1.bsatn.shunter" as const;
export const SHUNTER_DEFAULT_SUBPROTOCOL = SHUNTER_SUBPROTOCOL_V1;
export const SHUNTER_SUPPORTED_SUBPROTOCOLS = [SHUNTER_SUBPROTOCOL_V1] as const;
export const SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_SINGLE = 1 as const;
export const SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_SINGLE = 2 as const;
export const SHUNTER_CLIENT_MESSAGE_CALL_REDUCER = 3 as const;
export const SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_MULTI = 6 as const;
export const SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY = 7 as const;
export const SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW = 8 as const;
export const SHUNTER_SERVER_MESSAGE_IDENTITY_TOKEN = 1 as const;
export const SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED = 2 as const;
export const SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED = 3 as const;
export const SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR = 4 as const;
export const SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE = 6 as const;
export const SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE = 5 as const;
export const SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT = 8 as const;
export const SHUNTER_SERVER_MESSAGE_SUBSCRIBE_MULTI_APPLIED = 9 as const;
export const SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED = 10 as const;
export const SHUNTER_CALL_REDUCER_FLAGS_FULL_UPDATE = 0 as const;
export const SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY = 1 as const;

export type ShunterSubprotocol = typeof SHUNTER_SUBPROTOCOL_V1;

export interface ProtocolMetadata<Subprotocol extends string = string> {
  readonly minSupportedVersion: number;
  readonly currentVersion: number;
  readonly defaultSubprotocol: Subprotocol;
  readonly supportedSubprotocols: readonly Subprotocol[];
}

export const shunterProtocol = {
  minSupportedVersion: SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION,
  currentVersion: SHUNTER_CURRENT_PROTOCOL_VERSION,
  defaultSubprotocol: SHUNTER_DEFAULT_SUBPROTOCOL,
  supportedSubprotocols: SHUNTER_SUPPORTED_SUBPROTOCOLS,
} as const satisfies ProtocolMetadata<ShunterSubprotocol>;

export interface ProtocolCompatibilityIssue {
  readonly code:
    | "client_too_old"
    | "client_too_new"
    | "unsupported_default_subprotocol"
    | "unsupported_selected_subprotocol";
  readonly message: string;
  readonly receivedVersion?: number;
  readonly receivedSubprotocol?: string;
}

export type ProtocolCompatibilityResult =
  | { readonly ok: true; readonly subprotocol: ShunterSubprotocol }
  | { readonly ok: false; readonly issue: ProtocolCompatibilityIssue };

export type ShunterErrorKind =
  | "auth"
  | "validation"
  | "protocol"
  | "protocol_mismatch"
  | "transport"
  | "timeout"
  | "closed";

export interface ShunterErrorOptions {
  readonly code?: string;
  readonly details?: unknown;
  readonly cause?: unknown;
}

export class ShunterError extends Error {
  readonly kind: ShunterErrorKind;
  readonly code?: string;
  readonly details?: unknown;
  readonly cause?: unknown;

  constructor(kind: ShunterErrorKind, message: string, options: ShunterErrorOptions = {}) {
    super(message);
    this.name = new.target.name;
    this.kind = kind;
    this.code = options.code;
    this.details = options.details;
    this.cause = options.cause;
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class ShunterAuthError extends ShunterError {
  constructor(message: string, options: ShunterErrorOptions = {}) {
    super("auth", message, options);
  }
}

export class ShunterValidationError extends ShunterError {
  constructor(message: string, options: ShunterErrorOptions = {}) {
    super("validation", message, options);
  }
}

export class ShunterProtocolError extends ShunterError {
  constructor(message: string, options: ShunterErrorOptions = {}) {
    super("protocol", message, options);
  }
}

export interface ShunterProtocolMismatchErrorOptions extends ShunterErrorOptions {
  readonly expected: ProtocolMetadata;
  readonly receivedVersion?: number;
  readonly receivedSubprotocol?: string;
}

export class ShunterProtocolMismatchError extends ShunterError {
  readonly expected: ProtocolMetadata;
  readonly receivedVersion?: number;
  readonly receivedSubprotocol?: string;

  constructor(message: string, options: ShunterProtocolMismatchErrorOptions) {
    super("protocol_mismatch", message, options);
    this.expected = options.expected;
    this.receivedVersion = options.receivedVersion;
    this.receivedSubprotocol = options.receivedSubprotocol;
  }
}

export class ShunterTransportError extends ShunterError {
  constructor(message: string, options: ShunterErrorOptions = {}) {
    super("transport", message, options);
  }
}

export class ShunterTimeoutError extends ShunterError {
  constructor(message: string, options: ShunterErrorOptions = {}) {
    super("timeout", message, options);
  }
}

export class ShunterClosedClientError extends ShunterError {
  constructor(message: string, options: ShunterErrorOptions = {}) {
    super("closed", message, options);
  }
}

export function isShunterError(error: unknown): error is ShunterError {
  return error instanceof ShunterError;
}

export function toShunterError(
  error: unknown,
  kind: ShunterErrorKind = "transport",
  message = "Shunter operation failed",
): ShunterError {
  if (isShunterError(error)) {
    return error;
  }
  if (error instanceof Error) {
    return new ShunterError(kind, error.message || message, { cause: error });
  }
  return new ShunterError(kind, message, { cause: error });
}

export function checkProtocolCompatibility(
  protocol: ProtocolMetadata,
  selectedSubprotocol?: string,
): ProtocolCompatibilityResult {
  if (protocol.minSupportedVersion > SHUNTER_CURRENT_PROTOCOL_VERSION) {
    return {
      ok: false,
      issue: {
        code: "client_too_old",
        message: "Generated bindings require a newer Shunter protocol than this client runtime supports.",
        receivedVersion: protocol.minSupportedVersion,
      },
    };
  }
  if (protocol.currentVersion < SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION) {
    return {
      ok: false,
      issue: {
        code: "client_too_new",
        message: "Generated bindings target an older Shunter protocol than this client runtime supports.",
        receivedVersion: protocol.currentVersion,
      },
    };
  }
  if (!protocol.supportedSubprotocols.includes(protocol.defaultSubprotocol)) {
    return {
      ok: false,
      issue: {
        code: "unsupported_default_subprotocol",
        message: "Generated bindings declare a default subprotocol outside their supported subprotocol set.",
        receivedSubprotocol: protocol.defaultSubprotocol,
      },
    };
  }
  if (
    selectedSubprotocol !== undefined &&
    selectedSubprotocol !== SHUNTER_SUBPROTOCOL_V1
  ) {
    return {
      ok: false,
      issue: {
        code: "unsupported_selected_subprotocol",
        message: "The server selected an unsupported Shunter WebSocket subprotocol.",
        receivedSubprotocol: selectedSubprotocol,
      },
    };
  }
  if (!protocol.supportedSubprotocols.includes(SHUNTER_SUBPROTOCOL_V1)) {
    return {
      ok: false,
      issue: {
        code: "unsupported_default_subprotocol",
        message: "Generated bindings do not support this client runtime's Shunter WebSocket subprotocol.",
        receivedSubprotocol: protocol.defaultSubprotocol,
      },
    };
  }
  return { ok: true, subprotocol: SHUNTER_SUBPROTOCOL_V1 };
}

export function assertProtocolCompatible(
  protocol: ProtocolMetadata,
  selectedSubprotocol?: string,
): ShunterSubprotocol {
  const result = checkProtocolCompatibility(protocol, selectedSubprotocol);
  if (result.ok) {
    return result.subprotocol;
  }
  throw new ShunterProtocolMismatchError(result.issue.message, {
    code: result.issue.code,
    expected: protocol,
    receivedVersion: result.issue.receivedVersion,
    receivedSubprotocol: result.issue.receivedSubprotocol,
  });
}

export function selectShunterSubprotocol(protocol: ProtocolMetadata): ShunterSubprotocol {
  return assertProtocolCompatible(protocol);
}

export type ConnectionStatus =
  | "idle"
  | "connecting"
  | "connected"
  | "reconnecting"
  | "closing"
  | "closed"
  | "failed";

export interface GeneratedContractMetadata {
  readonly contractFormat: string;
  readonly contractVersion: number;
  readonly moduleName?: string;
  readonly moduleVersion?: string;
}

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

export type ConnectionState<Protocol extends ProtocolMetadata = ProtocolMetadata> =
  | { readonly status: "idle" }
  | { readonly status: "connecting"; readonly attempt: number }
  | { readonly status: "connected"; readonly metadata: ConnectionMetadata<Protocol> }
  | { readonly status: "reconnecting"; readonly attempt: number; readonly error?: ShunterError }
  | { readonly status: "closing" }
  | { readonly status: "closed"; readonly error?: ShunterError }
  | { readonly status: "failed"; readonly error: ShunterError };

export interface ConnectionStateChange<Protocol extends ProtocolMetadata = ProtocolMetadata> {
  readonly previous: ConnectionState<Protocol>;
  readonly current: ConnectionState<Protocol>;
}

export type ConnectionStateListener<Protocol extends ProtocolMetadata = ProtocolMetadata> = (
  change: ConnectionStateChange<Protocol>,
) => void;

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

export type WebSocketFactory = (
  url: string,
  protocols: readonly ShunterSubprotocol[],
) => WebSocketLike;

interface PendingReducerCall {
  readonly name: string;
  readonly cleanup?: () => void;
  resolve(value: Uint8Array): void;
  reject(error: ShunterError): void;
}

interface PendingDeclaredQuery {
  readonly name: string;
  readonly cleanup?: () => void;
  resolve(value: Uint8Array): void;
  reject(error: ShunterError): void;
}

interface PendingSubscription {
  readonly kind: "declared_view" | "table";
  readonly target: string;
  readonly requestId: RequestID;
  readonly queryId: QueryID;
  readonly tableName?: string;
  readonly onRawRows?: (message: SubscribeSingleAppliedMessage) => void;
  readonly onRawUpdate?: RawSubscriptionUpdateCallback;
  readonly onRows?: (rows: readonly unknown[]) => void;
  readonly onInitialRows?: (rows: readonly unknown[]) => void;
  readonly onUpdate?: (update: SubscriptionUpdate<unknown>) => void;
  readonly decodeRow?: RowDecoder<unknown>;
  readonly handle?: ManagedSubscriptionHandle<unknown>;
  readonly cleanup?: () => void;
  resolve(value: SubscriptionUnsubscribe | SubscriptionHandle<unknown>): void;
  reject(error: ShunterError): void;
}

interface ActiveSubscription {
  readonly kind: "declared_view" | "table";
  readonly target: string;
  readonly queryId: QueryID;
  readonly tableName?: string;
  readonly onRawRows?: (message: SubscribeSingleAppliedMessage) => void;
  readonly onRawUpdate?: RawSubscriptionUpdateCallback;
  readonly onRows?: (rows: readonly unknown[]) => void;
  readonly onInitialRows?: (rows: readonly unknown[]) => void;
  readonly onUpdate?: (update: SubscriptionUpdate<unknown>) => void;
  readonly decodeRow?: RowDecoder<unknown>;
  readonly handle?: ManagedSubscriptionHandle<unknown>;
  readonly rowCache?: Map<string, unknown[]>;
}

interface PendingUnsubscribe {
  readonly kind: "declared_view" | "table";
  readonly requestId: RequestID;
  readonly queryId: QueryID;
  resolve(): void;
  reject(error: ShunterError): void;
}

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

const closeNormalCode = 1000;
const maxUint32 = 0xffffffff;

interface NormalizedReconnectOptions {
  readonly enabled: boolean;
  readonly maxAttempts: number;
  readonly initialDelayMs: number;
  readonly maxDelayMs: number;
  readonly backoffMultiplier: number;
  readonly resubscribe: boolean;
}

export function createShunterClient<Protocol extends ProtocolMetadata>(
  options: RuntimeClientOptions<Protocol>,
): ShunterClient<Protocol> {
  const reconnectOptions = normalizeReconnectOptions(options.reconnect);
  let state: ConnectionState<Protocol> = { status: "idle" };
  let socket: WebSocketLike | undefined;
  let connectPromise: Promise<ConnectionMetadata<Protocol>> | undefined;
  let closePromise: Promise<void> | undefined;
  let disposed = false;
  let suppressSocketCloseTransition = false;
  let hasConnected = false;
  let connectGeneration = 0;
  let reconnectAttempt = 0;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let resolveClose: (() => void) | undefined;
  let rejectConnect: ((error: ShunterError) => void) | undefined;
  let connectClient: (() => Promise<ConnectionMetadata<Protocol>>) | undefined;
  let nextRequestId: RequestID = 1;
  let nextQueryId: QueryID = 1;
  const pendingReducerCalls = new Map<RequestID, PendingReducerCall>();
  const pendingDeclaredQueries = new Map<string, PendingDeclaredQuery>();
  const pendingSubscriptionsByRequest = new Map<RequestID, PendingSubscription>();
  const pendingSubscriptionsByQuery = new Map<QueryID, PendingSubscription>();
  const pendingUnsubscribesByRequest = new Map<RequestID, PendingUnsubscribe>();
  const pendingUnsubscribesByQuery = new Map<QueryID, PendingUnsubscribe>();
  const activeSubscriptionsByQuery = new Map<QueryID, ActiveSubscription>();
  const activeSubscriptionAliasesByRootQuery = new Map<QueryID, Set<QueryID>>();
  const listeners = new Set<ConnectionStateListener<Protocol>>();
  if (options.onStateChange !== undefined) {
    listeners.add(options.onStateChange);
  }

  const setState = (current: ConnectionState<Protocol>): void => {
    const previous = state;
    state = current;
    const change = { previous, current };
    for (const listener of [...listeners]) {
      listener(change);
    }
  };

  const cleanupSocketListeners = (
    ws: WebSocketLike,
    handlers: {
      open: (event: Event) => void;
      message: (event: MessageEvent) => void;
      close: (event: CloseEvent) => void;
      error: (event: Event) => void;
    },
  ): void => {
    ws.removeEventListener("open", handlers.open);
    ws.removeEventListener("message", handlers.message);
    ws.removeEventListener("close", handlers.close);
    ws.removeEventListener("error", handlers.error);
  };

  const rejectPendingReducerCalls = (error: ShunterError): void => {
    for (const [requestId, pending] of pendingReducerCalls) {
      pending.cleanup?.();
      pendingReducerCalls.delete(requestId);
      pending.reject(error);
    }
  };

  const rejectPendingDeclaredQueries = (error: ShunterError): void => {
    for (const [messageKey, pending] of pendingDeclaredQueries) {
      pending.cleanup?.();
      pendingDeclaredQueries.delete(messageKey);
      pending.reject(error);
    }
  };

  const cleanupPendingSubscription = (pending: PendingSubscription): void => {
    pending.cleanup?.();
    pendingSubscriptionsByRequest.delete(pending.requestId);
    pendingSubscriptionsByQuery.delete(pending.queryId);
  };

  const rejectPendingSubscriptions = (error: ShunterError): void => {
    for (const pending of [...pendingSubscriptionsByRequest.values()]) {
      cleanupPendingSubscription(pending);
      pending.handle?.close(error);
      pending.reject(error);
    }
  };

  const cleanupPendingUnsubscribe = (pending: PendingUnsubscribe): void => {
    pendingUnsubscribesByRequest.delete(pending.requestId);
    pendingUnsubscribesByQuery.delete(pending.queryId);
  };

  const rejectPendingUnsubscribes = (error: ShunterError): void => {
    for (const pending of [...pendingUnsubscribesByRequest.values()]) {
      cleanupPendingUnsubscribe(pending);
      removeActiveSubscription(pending.queryId);
      pending.reject(error);
    }
  };

  const rejectInFlightOperations = (error: ShunterError): void => {
    rejectPendingReducerCalls(error);
    rejectPendingDeclaredQueries(error);
    rejectPendingSubscriptions(error);
    rejectPendingUnsubscribes(error);
  };

  const closeActiveSubscriptions = (error: ShunterError): void => {
    for (const active of new Set(activeSubscriptionsByQuery.values())) {
      active.handle?.close(error);
    }
    activeSubscriptionsByQuery.clear();
    activeSubscriptionAliasesByRootQuery.clear();
  };

  const rejectPendingOperations = (error: ShunterError): void => {
    rejectInFlightOperations(error);
    closeActiveSubscriptions(error);
  };

  const clearReconnectTimer = (): void => {
    if (reconnectTimer === undefined) {
      return;
    }
    clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
  };

  const finishClose = (): void => {
    socket = undefined;
    connectPromise = undefined;
    closePromise = undefined;
    rejectConnect = undefined;
    resolveClose?.();
    resolveClose = undefined;
  };

  const failConnecting = (error: ShunterError): void => {
    suppressSocketCloseTransition = true;
    rejectConnect?.(error);
    setState({ status: "failed", error });
    try {
      socket?.close(closeNormalCode, "protocol failure");
    } catch {
      // Closing after a failed opening handshake is best-effort only.
    }
    finishClose();
  };

  const failConnected = (error: ShunterError): void => {
    suppressSocketCloseTransition = true;
    rejectPendingOperations(error);
    setState({ status: "failed", error });
    try {
      socket?.close(closeNormalCode, "protocol failure");
    } catch {
      // The connection is already failed; close is best-effort.
    }
    finishClose();
  };

  const scheduleReconnect = (error: ShunterError): boolean => {
    if (
      !reconnectOptions.enabled ||
      !hasConnected ||
      disposed ||
      state.status === "closing" ||
      state.status === "closed"
    ) {
      return false;
    }
    if (reconnectAttempt >= reconnectOptions.maxAttempts) {
      rejectConnect?.(error);
      rejectPendingOperations(error);
      setState({ status: "closed", error });
      finishClose();
      return true;
    }
    reconnectAttempt += 1;
    rejectConnect?.(error);
    rejectInFlightOperations(error);
    finishClose();
    setState({ status: "reconnecting", attempt: reconnectAttempt, error });
    const delay = reconnectDelayMs(reconnectOptions, reconnectAttempt);
    clearReconnectTimer();
    reconnectTimer = setTimeout(() => {
      reconnectTimer = undefined;
      void connectClient?.().then(() => {
        reconnectAttempt = 0;
      }).catch((connectError) => {
        const reconnectError = isShunterError(connectError)
          ? connectError
          : toShunterError(connectError, "transport", "Reconnect failed");
        scheduleReconnect(reconnectError);
      });
    }, delay);
    return true;
  };

  const beginClose = (code = closeNormalCode, reason = ""): Promise<void> => {
    clearReconnectTimer();
    reconnectAttempt = 0;
    connectGeneration += 1;
    if (closePromise !== undefined) {
      return closePromise;
    }
    if (state.status === "closed") {
      finishClose();
      return Promise.resolve();
    }
    const closedError = new ShunterClosedClientError("Shunter client connection is closing.");
    if (state.status === "failed") {
      rejectConnect?.(closedError);
      rejectPendingOperations(closedError);
      setState({ status: "closed" });
      finishClose();
      return Promise.resolve();
    }
    if (state.status === "idle") {
      setState({ status: "closed" });
      finishClose();
      return Promise.resolve();
    }
    rejectConnect?.(closedError);
    rejectPendingOperations(closedError);
    setState({ status: "closing" });
    const pendingClose = new Promise<void>((resolve) => {
      resolveClose = resolve;
    });
    closePromise = pendingClose;
    const closingSocket = socket;
    try {
      closingSocket?.close(code, reason);
    } catch (error) {
      setState({ status: "closed", error: toShunterError(error, "transport", "WebSocket close failed") });
      finishClose();
    }
    if (closingSocket === undefined) {
      setState({ status: "closed" });
      finishClose();
    }
    return pendingClose;
  };

  const allocateRequestId = (): RequestID => {
    const requestId = nextRequestId;
    nextRequestId = nextRequestId === maxUint32 ? 1 : nextRequestId + 1;
    return requestId;
  };

  const allocateQueryId = (): QueryID => {
    const queryId = nextQueryId;
    nextQueryId = nextQueryId === maxUint32 ? 1 : nextQueryId + 1;
    return queryId;
  };

  const settleReducerResponse = (update: TransactionUpdateMessage): void => {
    const { requestId, name } = update.reducerCall;
    const pending = pendingReducerCalls.get(requestId);
    if (pending === undefined) {
      return;
    }
    pending.cleanup?.();
    pendingReducerCalls.delete(requestId);
    if (name !== pending.name) {
      const error = new ShunterProtocolError("Reducer response did not match the pending request.", {
        details: { requestId, expectedName: pending.name, receivedName: name },
      });
      pending.reject(error);
      failConnected(error);
      return;
    }
    if (update.status.status === "failed") {
      pending.reject(new ShunterValidationError(update.status.error || "Reducer call failed.", {
        code: "reducer_failed",
        details: update,
      }));
      return;
    }
    const updateError = dispatchRawSubscriptionUpdates(update.status.updates, "TransactionUpdate");
    if (updateError !== undefined) {
      pending.reject(updateError);
      failConnected(updateError);
      return;
    }
    pending.resolve(update.rawFrame);
  };

  const settleDeclaredQueryResponse = (response: OneOffQueryResponseMessage): void => {
    const messageKey = bytesKey(response.messageId);
    const pending = pendingDeclaredQueries.get(messageKey);
    if (pending === undefined) {
      return;
    }
    pending.cleanup?.();
    pendingDeclaredQueries.delete(messageKey);
    if (response.error !== undefined) {
      pending.reject(new ShunterValidationError(response.error || "Declared query failed.", {
        code: "declared_query_failed",
        details: { name: pending.name, response },
      }));
      return;
    }
    pending.resolve(response.rawFrame);
  };

  const cloneRawSubscriptionUpdate = (update: RawSubscriptionUpdate): RawSubscriptionUpdate => ({
    queryId: update.queryId,
    tableName: update.tableName,
    inserts: new Uint8Array(update.inserts),
    deletes: new Uint8Array(update.deletes),
    ...(update.insertRowBytes === undefined
      ? {}
      : { insertRowBytes: update.insertRowBytes.map((row) => new Uint8Array(row)) }),
    ...(update.deleteRowBytes === undefined
      ? {}
      : { deleteRowBytes: update.deleteRowBytes.map((row) => new Uint8Array(row)) }),
  });

  const cloneRowBytes = (rows: readonly Uint8Array[]): Uint8Array[] =>
    rows.map((row) => new Uint8Array(row));

  const decodeSubscriptionRows = (
    rows: readonly Uint8Array[] | undefined,
    decodeRow: RowDecoder<unknown>,
    label: string,
  ): unknown[] => {
    if (rows === undefined) {
      throw new ShunterProtocolError(`${label} rows were not encoded as a RowList.`);
    }
    return rows.map((row, rowIndex) => {
      try {
        return decodeRow(new Uint8Array(row));
      } catch (error) {
        if (isShunterError(error)) {
          throw error;
        }
        throw new ShunterValidationError(`${label} row decoder failed.`, {
          cause: error,
          details: { rowIndex },
        });
      }
    });
  };

  const decodeSubscriptionInitialRows = (
    updates: readonly RawSubscriptionUpdate[],
    decodeRow: RowDecoder<unknown>,
    label: string,
  ): unknown[] => updates.flatMap((update) =>
    decodeSubscriptionRows(update.insertRowBytes, decodeRow, `${label} initial`)
  );

  const cacheRow = (rowCache: Map<string, unknown[]>, rowBytes: Uint8Array, row: unknown): void => {
    const key = bytesKey(rowBytes);
    const bucket = rowCache.get(key);
    if (bucket === undefined) {
      rowCache.set(key, [row]);
      return;
    }
    bucket.push(row);
  };

  const deleteCachedRow = (rowCache: Map<string, unknown[]>, rowBytes: Uint8Array): void => {
    const key = bytesKey(rowBytes);
    const bucket = rowCache.get(key);
    if (bucket === undefined) {
      return;
    }
    bucket.shift();
    if (bucket.length === 0) {
      rowCache.delete(key);
    }
  };

  const cachedRows = (rowCache: Map<string, unknown[]>): unknown[] => [...rowCache.values()].flat();

  const replaceCachedRows = (
    active: ActiveSubscription,
    rowBytes: readonly Uint8Array[],
    rows: readonly unknown[],
  ): void => {
    if (active.rowCache === undefined) {
      return;
    }
    active.rowCache.clear();
    for (let i = 0; i < rowBytes.length; i += 1) {
      cacheRow(active.rowCache, rowBytes[i], rows[i]);
    }
    active.handle?.replaceRows(cachedRows(active.rowCache));
  };

  const applyCachedUpdate = (
    active: ActiveSubscription,
    update: RawSubscriptionUpdate,
    inserts: readonly unknown[] | undefined,
    label: string,
  ): void => {
    if (active.rowCache === undefined || active.handle === undefined) {
      return;
    }
    if (update.insertRowBytes === undefined || update.deleteRowBytes === undefined) {
      if (active.decodeRow !== undefined || active.onUpdate !== undefined) {
        throw new ShunterProtocolError(`${label} rows were not encoded as a RowList.`);
      }
      return;
    }
    for (const rowBytes of update.deleteRowBytes) {
      deleteCachedRow(active.rowCache, rowBytes);
    }
    if (active.decodeRow === undefined) {
      for (const rowBytes of update.insertRowBytes) {
        cacheRow(active.rowCache, rowBytes, new Uint8Array(rowBytes));
      }
    } else {
      const decodedInserts = inserts ?? decodeSubscriptionRows(update.insertRowBytes, active.decodeRow, `${label} insert`);
      for (let i = 0; i < update.insertRowBytes.length; i += 1) {
        cacheRow(active.rowCache, update.insertRowBytes[i], decodedInserts[i]);
      }
    }
    active.handle.replaceRows(cachedRows(active.rowCache));
  };

  const registerActiveSubscription = (
    active: ActiveSubscription,
    aliases: Iterable<QueryID> = [active.queryId],
  ): void => {
    const previousAliases = activeSubscriptionAliasesByRootQuery.get(active.queryId);
    if (previousAliases !== undefined) {
      for (const alias of previousAliases) {
        activeSubscriptionsByQuery.delete(alias);
      }
    }
    const activeAliases = new Set<QueryID>([active.queryId, ...aliases]);
    activeSubscriptionAliasesByRootQuery.set(active.queryId, activeAliases);
    for (const alias of activeAliases) {
      activeSubscriptionsByQuery.set(alias, active);
    }
  };

  const removeActiveSubscription = (queryId: QueryID): void => {
    const active = activeSubscriptionsByQuery.get(queryId);
    const rootQueryId = active?.queryId ?? queryId;
    const aliases = activeSubscriptionAliasesByRootQuery.get(rootQueryId) ?? new Set<QueryID>([queryId]);
    for (const alias of aliases) {
      activeSubscriptionsByQuery.delete(alias);
    }
    activeSubscriptionAliasesByRootQuery.delete(rootQueryId);
  };

  const dispatchRawSubscriptionUpdates = (
    updates: readonly RawSubscriptionUpdate[],
    label: string,
  ): ShunterError | undefined => {
    for (const update of updates) {
      const active = activeSubscriptionsByQuery.get(update.queryId);
      if (active === undefined) {
        continue;
      }
      if (active.onRawUpdate !== undefined) {
        try {
          active.onRawUpdate(cloneRawSubscriptionUpdate(update));
        } catch (error) {
          return toShunterError(error, "validation", `${label} raw subscription update callback failed`);
        }
      }
      if (active.onUpdate !== undefined && active.decodeRow !== undefined) {
        let inserts: readonly unknown[] | undefined;
        let deletes: readonly unknown[] | undefined;
        try {
          inserts = decodeSubscriptionRows(update.insertRowBytes, active.decodeRow, `${label} insert`);
          deletes = decodeSubscriptionRows(update.deleteRowBytes, active.decodeRow, `${label} delete`);
          active.onUpdate({
            queryId: update.queryId,
            tableName: update.tableName,
            inserts,
            deletes,
          });
          applyCachedUpdate(active, update, inserts, label);
        } catch (error) {
          return toShunterError(error, "validation", `${label} subscription update callback failed`);
        }
      } else {
        try {
          applyCachedUpdate(active, update, undefined, label);
        } catch (error) {
          return toShunterError(error, "validation", `${label} subscription cache update failed`);
        }
      }
    }
    return undefined;
  };

  const pendingSubscriptionForResponse = (
    requestId: RequestID | undefined,
    queryId: QueryID | undefined,
    label: string,
  ): PendingSubscription | undefined => {
    const requestPending =
      requestId === undefined ? undefined : pendingSubscriptionsByRequest.get(requestId);
    const queryPending =
      queryId === undefined ? undefined : pendingSubscriptionsByQuery.get(queryId);
    if (requestPending !== undefined && queryPending !== undefined && requestPending !== queryPending) {
      failConnected(new ShunterProtocolError(`${label} response matched multiple pending subscriptions.`, {
        details: { requestId, queryId },
      }));
      return undefined;
    }
    const pending = requestPending ?? queryPending;
    if (pending === undefined) {
      return undefined;
    }
    if (
      (requestId !== undefined && requestId !== pending.requestId) ||
      (queryId !== undefined && queryId !== pending.queryId)
    ) {
      failConnected(new ShunterProtocolError(`${label} response did not match the pending subscription.`, {
        details: {
          expectedRequestId: pending.requestId,
          expectedQueryId: pending.queryId,
          receivedRequestId: requestId,
          receivedQueryId: queryId,
        },
      }));
      return undefined;
    }
    return pending;
  };

  const pendingUnsubscribeForResponse = (
    requestId: RequestID | undefined,
    queryId: QueryID | undefined,
    label: string,
  ): PendingUnsubscribe | undefined => {
    const requestPending =
      requestId === undefined ? undefined : pendingUnsubscribesByRequest.get(requestId);
    const queryPending =
      queryId === undefined ? undefined : pendingUnsubscribesByQuery.get(queryId);
    if (requestPending !== undefined && queryPending !== undefined && requestPending !== queryPending) {
      failConnected(new ShunterProtocolError(`${label} response matched multiple pending unsubscribes.`, {
        details: { requestId, queryId },
      }));
      return undefined;
    }
    const pending = requestPending ?? queryPending;
    if (pending === undefined) {
      return undefined;
    }
    if (
      (requestId !== undefined && requestId !== pending.requestId) ||
      (queryId !== undefined && queryId !== pending.queryId)
    ) {
      failConnected(new ShunterProtocolError(`${label} response did not match the pending unsubscribe.`, {
        details: {
          expectedRequestId: pending.requestId,
          expectedQueryId: pending.queryId,
          receivedRequestId: requestId,
          receivedQueryId: queryId,
        },
      }));
      return undefined;
    }
    return pending;
  };

  const unsubscribeOnce = (
    kind: PendingUnsubscribe["kind"],
    queryId: QueryID,
    encodeRequest: (queryId: QueryID, options: { readonly requestId?: RequestID }) => EncodedSubscriptionUnsubscribeRequest,
    closedMessage: string,
    sendMessage: string,
  ): SubscriptionUnsubscribe => {
    let unsubscribePromise: Promise<void> | undefined;
    return () => {
      if (unsubscribePromise !== undefined) {
        return unsubscribePromise;
      }
      unsubscribePromise = (async () => {
        const activeSocket = socket;
        if (state.status === "reconnecting" || state.status === "connecting") {
          removeActiveSubscription(queryId);
          return;
        }
        if (state.status !== "connected" || activeSocket === undefined) {
          throw new ShunterClosedClientError(closedMessage);
        }
        const request = encodeRequest(queryId, {
          requestId: allocateRequestId(),
        });
        if (
          pendingUnsubscribesByRequest.has(request.requestId) ||
          pendingUnsubscribesByQuery.has(request.queryId)
        ) {
          throw new ShunterValidationError("Unsubscribe request ID is already in flight.", {
            details: { kind, requestId: request.requestId, queryId: request.queryId },
          });
        }
        await new Promise<void>((resolve, reject) => {
          const pendingUnsubscribe: PendingUnsubscribe = {
            kind,
            requestId: request.requestId,
            queryId: request.queryId,
            resolve,
            reject,
          };
          pendingUnsubscribesByRequest.set(request.requestId, pendingUnsubscribe);
          pendingUnsubscribesByQuery.set(request.queryId, pendingUnsubscribe);
          removeActiveSubscription(request.queryId);
          try {
            activeSocket.send(request.frame);
          } catch (error) {
            cleanupPendingUnsubscribe(pendingUnsubscribe);
            reject(toShunterError(error, "transport", sendMessage));
          }
        });
      })();
      return unsubscribePromise;
    };
  };

  const declaredViewUnsubscribe = (queryId: QueryID): SubscriptionUnsubscribe =>
    unsubscribeOnce(
      "declared_view",
      queryId,
      encodeUnsubscribeMultiRequest,
      "Cannot unsubscribe after the Shunter client is disconnected.",
      "Unsubscribe request send failed",
    );

  const tableSubscriptionUnsubscribe = (queryId: QueryID): SubscriptionUnsubscribe =>
    unsubscribeOnce(
      "table",
      queryId,
      encodeUnsubscribeSingleRequest,
      "Cannot unsubscribe a table subscription after the Shunter client is disconnected.",
      "Table unsubscribe request send failed",
    );

  const resubscribeActiveSubscriptions = (activeSocket: WebSocketLike): ShunterError | undefined => {
    if (!reconnectOptions.resubscribe) {
      return undefined;
    }
    for (const active of new Set(activeSubscriptionsByQuery.values())) {
      const request = active.kind === "table"
        ? encodeTableSubscriptionRequest(active.tableName ?? active.target, {
          requestId: allocateRequestId(),
          queryId: active.queryId,
        })
        : encodeDeclaredViewSubscriptionRequest(active.target, {
          requestId: allocateRequestId(),
          queryId: active.queryId,
        });
      if (
        pendingSubscriptionsByRequest.has(request.requestId) ||
        pendingSubscriptionsByQuery.has(request.queryId)
      ) {
        return new ShunterValidationError("Reconnect subscription ID is already in flight.", {
          details: {
            kind: active.kind,
            target: active.target,
            requestId: request.requestId,
            queryId: request.queryId,
          },
        });
      }
      const pending: PendingSubscription = {
        kind: active.kind,
        target: active.target,
        requestId: request.requestId,
        queryId: request.queryId,
        tableName: active.tableName,
        onRawRows: active.onRawRows,
        onRawUpdate: active.onRawUpdate,
        onRows: active.onRows,
        onInitialRows: active.onInitialRows,
        onUpdate: active.onUpdate,
        decodeRow: active.decodeRow,
        handle: active.handle,
        resolve: () => {},
        reject: (error) => {
          removeActiveSubscription(active.queryId);
          active.handle?.close(error);
        },
      };
      pendingSubscriptionsByRequest.set(request.requestId, pending);
      pendingSubscriptionsByQuery.set(request.queryId, pending);
      try {
        activeSocket.send(request.frame);
      } catch (error) {
        cleanupPendingSubscription(pending);
        return toShunterError(error, "transport", "Reconnect subscription request send failed");
      }
    }
    return undefined;
  };

  const settleTableSubscriptionApplied = (response: SubscribeSingleAppliedMessage): void => {
    const pending = pendingSubscriptionForResponse(
      response.requestId,
      response.queryId,
      "SubscribeSingleApplied",
    );
    if (pending === undefined) {
      return;
    }
    if (pending.kind !== "table" || pending.tableName !== response.tableName) {
      failConnected(new ShunterProtocolError("SubscribeSingleApplied response did not match the pending table subscription.", {
        details: {
          expectedKind: pending.kind,
          expectedTableName: pending.tableName,
          receivedTableName: response.tableName,
          response,
        },
      }));
      return;
    }
    const active: ActiveSubscription = {
      kind: pending.kind,
      target: pending.target,
      queryId: response.queryId,
      tableName: response.tableName,
      onRawRows: pending.onRawRows,
      onRawUpdate: pending.onRawUpdate,
      onRows: pending.onRows,
      onInitialRows: pending.onInitialRows,
      onUpdate: pending.onUpdate,
      decodeRow: pending.decodeRow,
      handle: pending.handle,
      rowCache: pending.handle === undefined ? undefined : new Map<string, unknown[]>(),
    };
    registerActiveSubscription(active);
    if (pending.onRawRows !== undefined) {
      try {
        pending.onRawRows({
          ...response,
          rows: new Uint8Array(response.rows),
          rawFrame: new Uint8Array(response.rawFrame),
        });
      } catch (error) {
        const callbackError = toShunterError(error, "validation", "SubscribeSingleApplied raw rows callback failed");
        removeActiveSubscription(response.queryId);
        cleanupPendingSubscription(pending);
        pending.handle?.close(callbackError);
        pending.reject(callbackError);
        failConnected(callbackError);
        return;
      }
    }
    if (pending.decodeRow !== undefined) {
      try {
        const rows = decodeSubscriptionRows(response.rowBytes, pending.decodeRow, "SubscribeSingleApplied initial");
        replaceCachedRows(active, response.rowBytes, rows);
        pending.onRows?.(rows);
        pending.onInitialRows?.(rows);
      } catch (error) {
        const callbackError = toShunterError(error, "validation", "SubscribeSingleApplied row callback failed");
        removeActiveSubscription(response.queryId);
        cleanupPendingSubscription(pending);
        pending.handle?.close(callbackError);
        pending.reject(callbackError);
        failConnected(callbackError);
        return;
      }
    } else {
      replaceCachedRows(active, response.rowBytes, cloneRowBytes(response.rowBytes));
    }
    cleanupPendingSubscription(pending);
    pending.resolve(pending.handle ?? tableSubscriptionUnsubscribe(response.queryId));
  };

  const settleDeclaredViewSubscriptionApplied = (response: SubscriptionSetAppliedMessage): void => {
    const pending = pendingSubscriptionForResponse(
      response.requestId,
      response.queryId,
      "SubscribeMultiApplied",
    );
    if (pending === undefined) {
      return;
    }
    if (pending.kind !== "declared_view") {
      failConnected(new ShunterProtocolError("SubscribeMultiApplied response did not match the pending declared view subscription.", {
        details: { expectedKind: pending.kind, response },
      }));
      return;
    }
    registerActiveSubscription({
      kind: pending.kind,
      target: pending.target,
      queryId: response.queryId,
      onRawUpdate: pending.onRawUpdate,
      onUpdate: pending.onUpdate,
      decodeRow: pending.decodeRow,
      handle: pending.handle,
      rowCache: pending.handle === undefined ? undefined : new Map(),
    }, response.updates.map((update) => update.queryId));
    pending.handle?.replaceRows([]);
    if (pending.onInitialRows !== undefined && pending.decodeRow !== undefined) {
      try {
        pending.onInitialRows(decodeSubscriptionInitialRows(response.updates, pending.decodeRow, "SubscribeMultiApplied"));
      } catch (error) {
        const callbackError = toShunterError(error, "validation", "SubscribeMultiApplied initial row callback failed");
        removeActiveSubscription(response.queryId);
        cleanupPendingSubscription(pending);
        pending.handle?.close(callbackError);
        pending.reject(callbackError);
        failConnected(callbackError);
        return;
      }
    }
    const updateError = dispatchRawSubscriptionUpdates(response.updates, "SubscribeMultiApplied");
    if (updateError !== undefined) {
      removeActiveSubscription(response.queryId);
      cleanupPendingSubscription(pending);
      pending.handle?.close(updateError);
      pending.reject(updateError);
      failConnected(updateError);
      return;
    }
    cleanupPendingSubscription(pending);
    pending.resolve(pending.handle ?? declaredViewUnsubscribe(response.queryId));
  };

  const settleSubscriptionError = (response: SubscriptionErrorMessage): void => {
    const pending = pendingSubscriptionForResponse(
      response.requestId,
      response.queryId,
      "SubscriptionError",
    );
    if (pending === undefined) {
      return;
    }
    cleanupPendingSubscription(pending);
    const error = new ShunterValidationError(response.error || "Subscription failed.", {
      code: "subscription_failed",
      details: { kind: pending.kind, target: pending.target, response },
    });
    pending.handle?.close(error);
    pending.reject(error);
  };

  const settleUnsubscribeError = (response: SubscriptionErrorMessage): void => {
    const pending = pendingUnsubscribeForResponse(
      response.requestId,
      response.queryId,
      "SubscriptionError",
    );
    if (pending === undefined) {
      return;
    }
    cleanupPendingUnsubscribe(pending);
    removeActiveSubscription(pending.queryId);
    pending.reject(new ShunterValidationError(response.error || "Unsubscribe failed.", {
      code: "unsubscribe_failed",
      details: { kind: pending.kind, response },
    }));
  };

  const settleUnsubscribeApplied = (
    response: { readonly requestId: RequestID; readonly queryId: QueryID },
    kind: PendingUnsubscribe["kind"],
    label: string,
  ): void => {
    const pending = pendingUnsubscribeForResponse(response.requestId, response.queryId, label);
    if (pending === undefined) {
      return;
    }
    if (pending.kind !== kind) {
      failConnected(new ShunterProtocolError(`${label} response did not match the pending unsubscribe kind.`, {
        details: { expectedKind: pending.kind, receivedKind: kind, response },
      }));
      return;
    }
    cleanupPendingUnsubscribe(pending);
    removeActiveSubscription(response.queryId);
    pending.resolve();
  };

  const settleTransactionUpdateLight = (update: TransactionUpdateLightMessage): void => {
    const updateError = dispatchRawSubscriptionUpdates(update.updates, "TransactionUpdateLight");
    if (updateError !== undefined) {
      failConnected(updateError);
    }
  };

  const handleConnectedMessage = (event: MessageEvent): void => {
    let frame: Uint8Array;
    try {
      frame = frameBytes(event.data);
    } catch (error) {
      failConnected(isShunterError(error) ? error : toShunterError(error, "protocol", "Decode server frame failed"));
      return;
    }
    try {
      switch (frame[0]) {
        case SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED:
          settleTableSubscriptionApplied(decodeSubscribeSingleAppliedFrame(frame));
          return;
        case SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED:
          settleUnsubscribeApplied(decodeUnsubscribeSingleAppliedFrame(frame), "table", "UnsubscribeSingleApplied");
          return;
        case SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE:
          settleReducerResponse(decodeTransactionUpdateFrame(frame));
          return;
        case SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT:
          settleTransactionUpdateLight(decodeTransactionUpdateLightFrame(frame));
          return;
        case SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE:
          settleDeclaredQueryResponse(decodeOneOffQueryResponseFrame(frame));
          return;
        case SHUNTER_SERVER_MESSAGE_SUBSCRIBE_MULTI_APPLIED:
          settleDeclaredViewSubscriptionApplied(decodeSubscribeMultiAppliedFrame(frame));
          return;
        case SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR:
          {
            const response = decodeSubscriptionErrorFrame(frame);
            settleSubscriptionError(response);
            settleUnsubscribeError(response);
          }
          return;
        case SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED:
          settleUnsubscribeApplied(
            decodeUnsubscribeMultiAppliedFrame(frame),
            "declared_view",
            "UnsubscribeMultiApplied",
          );
          return;
        default:
          return;
      }
    } catch (error) {
      failConnected(isShunterError(error) ? error : toShunterError(error, "protocol", "Decode server response failed"));
    }
  };

  connectClient = async (): Promise<ConnectionMetadata<Protocol>> => {
      if (disposed) {
        throw new ShunterClosedClientError("Cannot connect a disposed Shunter client.");
      }
      if (state.status === "connected") {
        return state.metadata;
      }
      if (connectPromise !== undefined) {
        return connectPromise;
      }

      const attempt = state.status === "reconnecting" ? state.attempt : 1;
      const generation = connectGeneration + 1;
      connectGeneration = generation;
      setState({ status: "connecting", attempt });

      connectPromise = new Promise<ConnectionMetadata<Protocol>>(async (resolve, reject) => {
        rejectConnect = reject;
        let offeredSubprotocol: ShunterSubprotocol;
        let url: string;
        let tokenAwaitStarted = false;
        try {
          offeredSubprotocol = selectShunterSubprotocol(options.protocol);
          tokenAwaitStarted = true;
          const token = await resolveToken(options.token);
          if (connectGeneration !== generation || state.status !== "connecting") {
            return;
          }
          url = withTokenQuery(options.url, token);
          if (options.signal?.aborted) {
            throw new ShunterClosedClientError("Connection aborted before opening.");
          }
        } catch (error) {
          if (tokenAwaitStarted && (connectGeneration !== generation || state.status !== "connecting")) {
            return;
          }
          const shunterError =
            isShunterError(error)
              ? error
              : new ShunterAuthError("Token provider failed.", { cause: error });
          setState({ status: "failed", error: shunterError });
          finishClose();
          reject(shunterError);
          return;
        }

        let ws: WebSocketLike;
        try {
          ws = createWebSocket(url, [offeredSubprotocol], options.webSocketFactory);
        } catch (error) {
          const shunterError = toShunterError(error, "transport", "Create WebSocket failed");
          setState({ status: "failed", error: shunterError });
          finishClose();
          reject(shunterError);
          return;
        }

        socket = ws;
        suppressSocketCloseTransition = false;
        ws.binaryType = "arraybuffer";
        let selectedSubprotocol: string | undefined;
        let cleanupConnectAbort: (() => void) | undefined;
        const cleanupOpeningAbort = (): void => {
          cleanupConnectAbort?.();
          cleanupConnectAbort = undefined;
        };
        const handlers = {
          open: (): void => {
            if (socket !== ws) {
              return;
            }
            try {
              selectedSubprotocol = ws.protocol;
              assertProtocolCompatible(options.protocol, selectedSubprotocol);
            } catch (error) {
              cleanupOpeningAbort();
              failConnecting(
                isShunterError(error)
                  ? error
                  : toShunterError(error, "protocol", "Protocol negotiation failed"),
              );
            }
          },
          message: (event: MessageEvent): void => {
            if (socket !== ws) {
              return;
            }
            if (state.status === "connected") {
              handleConnectedMessage(event);
              return;
            }
            if (state.status !== "connecting") {
              return;
            }
            try {
              const identityToken = decodeIdentityTokenFrame(event.data);
              const metadata: ConnectionMetadata<Protocol> = {
                protocol: options.protocol,
                subprotocol: selectedSubprotocol ?? ws.protocol ?? offeredSubprotocol,
                identityToken: identityToken.token,
                identity: identityToken.identity,
                connectionId: identityToken.connectionId,
              };
              cleanupOpeningAbort();
              hasConnected = true;
              setState({ status: "connected", metadata });
              const resubscribeError = resubscribeActiveSubscriptions(ws);
              if (resubscribeError !== undefined) {
                failConnected(resubscribeError);
                reject(resubscribeError);
                return;
              }
              resolve(metadata);
            } catch (error) {
              cleanupOpeningAbort();
              failConnecting(
                isShunterError(error)
                  ? error
                  : toShunterError(error, "protocol", "Decode IdentityToken failed"),
              );
            }
          },
          close: (event: CloseEvent): void => {
            cleanupSocketListeners(ws, handlers);
            cleanupOpeningAbort();
            if (socket !== ws) {
              return;
            }
            if (suppressSocketCloseTransition) {
              return;
            }
            if (state.status === "connecting") {
              const error = new ShunterTransportError("WebSocket closed before opening.", {
                code: String(event.code),
                details: { reason: event.reason, wasClean: event.wasClean },
              });
              if (!scheduleReconnect(error)) {
                setState({ status: "failed", error });
                rejectPendingOperations(error);
                reject(error);
                finishClose();
              }
            } else if (state.status === "closing") {
              setState({ status: "closed" });
              finishClose();
            } else if (state.status !== "closed") {
              const error = new ShunterClosedClientError("Shunter client connection closed.", {
                code: String(event.code),
                details: { reason: event.reason, wasClean: event.wasClean },
              });
              if (!scheduleReconnect(error)) {
                rejectPendingOperations(error);
                setState({ status: "closed", error });
                finishClose();
              }
            }
          },
          error: (event: Event): void => {
            if (socket !== ws) {
              return;
            }
            if (state.status === "connecting") {
              cleanupOpeningAbort();
              const error = new ShunterTransportError("WebSocket failed before opening.", {
                details: event,
              });
              if (!scheduleReconnect(error)) {
                setState({ status: "failed", error });
                reject(error);
                finishClose();
              }
              suppressSocketCloseTransition = true;
              try {
                ws.close(closeNormalCode, "open failed");
              } catch {
                // Nothing useful can be recovered from a failed close here.
              }
            } else if (state.status === "connected") {
              const error = new ShunterTransportError("WebSocket failed.", {
                details: event,
              });
              suppressSocketCloseTransition = true;
              if (!scheduleReconnect(error)) {
                rejectPendingOperations(error);
                setState({ status: "failed", error });
                finishClose();
              }
              try {
                ws.close(closeNormalCode, "transport failure");
              } catch {
                // Nothing useful can be recovered from a failed close here.
              }
            }
          },
        };
        const abortOpening = (): void => {
          if (state.status !== "connecting" || socket !== ws) {
            return;
          }
          const error = new ShunterClosedClientError("Connection aborted before opening.");
          cleanupOpeningAbort();
          cleanupSocketListeners(ws, handlers);
          suppressSocketCloseTransition = true;
          setState({ status: "failed", error });
          reject(error);
          try {
            ws.close(closeNormalCode, "connection aborted");
          } catch {
            // The caller already aborted the opening handshake.
          }
          finishClose();
        };
        if (options.signal !== undefined) {
          if (options.signal.aborted) {
            abortOpening();
            return;
          }
          options.signal.addEventListener("abort", abortOpening, { once: true });
          cleanupConnectAbort = () => {
            options.signal?.removeEventListener("abort", abortOpening);
          };
        }
        ws.addEventListener("open", handlers.open);
        ws.addEventListener("message", handlers.message);
        ws.addEventListener("close", handlers.close);
        ws.addEventListener("error", handlers.error);
      });

      return connectPromise;
  };

  return {
    get state() {
      return state;
    },
    connect: connectClient,
    async callReducer(name: string, args: Uint8Array, options: ReducerCallOptions = {}): Promise<Uint8Array> {
      if (disposed) {
        throw new ShunterClosedClientError("Cannot call a reducer on a disposed Shunter client.");
      }
      if (options.signal?.aborted) {
        throw new ShunterClosedClientError("Reducer call aborted before sending.");
      }
      const activeSocket = socket;
      if (state.status !== "connected" || activeSocket === undefined) {
        throw new ShunterClosedClientError("Cannot call a reducer before the Shunter client is connected.");
      }
      const request = encodeReducerCallRequest(name, args, {
        ...options,
        requestId: options.requestId ?? allocateRequestId(),
      });
      if (request.flags === SHUNTER_CALL_REDUCER_FLAGS_FULL_UPDATE && pendingReducerCalls.has(request.requestId)) {
        throw new ShunterValidationError("Reducer request ID is already in flight.", {
          details: { requestId: request.requestId },
        });
      }
      if (request.flags === SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY) {
        try {
          activeSocket.send(request.frame);
        } catch (error) {
          throw toShunterError(error, "transport", "Reducer request send failed");
        }
        return request.frame;
      }
      return new Promise<Uint8Array>((resolve, reject) => {
        let cleanup: (() => void) | undefined;
        if (options.signal !== undefined) {
          const abort = (): void => {
            pendingReducerCalls.delete(request.requestId);
            cleanup?.();
            reject(new ShunterClosedClientError("Reducer call aborted before a response was received."));
          };
          options.signal.addEventListener("abort", abort, { once: true });
          cleanup = () => {
            options.signal?.removeEventListener("abort", abort);
          };
        }
        pendingReducerCalls.set(request.requestId, {
          name,
          cleanup,
          resolve,
          reject,
        });
        try {
          activeSocket.send(request.frame);
        } catch (error) {
          pendingReducerCalls.delete(request.requestId);
          cleanup?.();
          reject(toShunterError(error, "transport", "Reducer request send failed"));
        }
      });
    },
    async runDeclaredQuery(name: string, options: DeclaredQueryOptions = {}): Promise<Uint8Array> {
      if (disposed) {
        throw new ShunterClosedClientError("Cannot run a declared query on a disposed Shunter client.");
      }
      if (options.signal?.aborted) {
        throw new ShunterClosedClientError("Declared query aborted before sending.");
      }
      const activeSocket = socket;
      if (state.status !== "connected" || activeSocket === undefined) {
        throw new ShunterClosedClientError("Cannot run a declared query before the Shunter client is connected.");
      }
      const request = encodeDeclaredQueryRequest(name, {
        ...options,
        requestId: options.messageId === undefined ? options.requestId ?? allocateRequestId() : options.requestId,
      });
      const messageKey = bytesKey(request.messageId);
      if (pendingDeclaredQueries.has(messageKey)) {
        throw new ShunterValidationError("Declared query message ID is already in flight.", {
          details: { name, messageId: request.messageId },
        });
      }
      return new Promise<Uint8Array>((resolve, reject) => {
        let cleanup: (() => void) | undefined;
        if (options.signal !== undefined) {
          const abort = (): void => {
            pendingDeclaredQueries.delete(messageKey);
            cleanup?.();
            reject(new ShunterClosedClientError("Declared query aborted before a response was received."));
          };
          options.signal.addEventListener("abort", abort, { once: true });
          cleanup = () => {
            options.signal?.removeEventListener("abort", abort);
          };
        }
        pendingDeclaredQueries.set(messageKey, {
          name,
          cleanup,
          resolve,
          reject,
        });
        try {
          activeSocket.send(request.frame);
        } catch (error) {
          pendingDeclaredQueries.delete(messageKey);
          cleanup?.();
          reject(toShunterError(error, "transport", "Declared query request send failed"));
        }
      });
    },
    subscribeDeclaredView: (async (
      name: string,
      options: DeclaredViewSubscriptionOptions = {},
    ): Promise<SubscriptionUnsubscribe | SubscriptionHandle<Uint8Array>> => {
      if (disposed) {
        throw new ShunterClosedClientError("Cannot subscribe a declared view on a disposed Shunter client.");
      }
      if (options.signal?.aborted) {
        throw new ShunterClosedClientError("Declared view subscription aborted before sending.");
      }
      const activeSocket = socket;
      if (state.status !== "connected" || activeSocket === undefined) {
        throw new ShunterClosedClientError("Cannot subscribe a declared view before the Shunter client is connected.");
      }
      const request = encodeDeclaredViewSubscriptionRequest(name, {
        ...options,
        requestId: options.requestId ?? allocateRequestId(),
        queryId: options.queryId ?? allocateQueryId(),
      });
      if (
        pendingSubscriptionsByRequest.has(request.requestId) ||
        pendingSubscriptionsByQuery.has(request.queryId)
      ) {
        throw new ShunterValidationError("Declared view subscription ID is already in flight.", {
          details: { name, requestId: request.requestId, queryId: request.queryId },
        });
      }
      const handle = options.returnHandle === true
        ? createSubscriptionHandle<unknown>({
          queryId: request.queryId,
          unsubscribe: declaredViewUnsubscribe(request.queryId),
        })
        : undefined;
      return new Promise<SubscriptionUnsubscribe | SubscriptionHandle<Uint8Array>>((resolve, reject) => {
        let cleanup: (() => void) | undefined;
        if (options.signal !== undefined) {
          const abort = (): void => {
            const pending = pendingSubscriptionsByRequest.get(request.requestId);
            if (pending !== undefined) {
              cleanupPendingSubscription(pending);
            }
            cleanup?.();
            const abortError = new ShunterClosedClientError("Declared view subscription aborted before a response was received.");
            handle?.close(abortError);
            reject(abortError);
          };
          options.signal.addEventListener("abort", abort, { once: true });
          cleanup = () => {
            options.signal?.removeEventListener("abort", abort);
          };
        }
        const pending: PendingSubscription = {
          kind: "declared_view",
          target: name,
          requestId: request.requestId,
          queryId: request.queryId,
          onInitialRows: options.onInitialRows as ((rows: readonly unknown[]) => void) | undefined,
          onUpdate: options.onUpdate as ((update: SubscriptionUpdate<unknown>) => void) | undefined,
          onRawUpdate: options.onRawUpdate,
          decodeRow: options.decodeRow as RowDecoder<unknown> | undefined,
          handle,
          cleanup,
          resolve: resolve as (value: SubscriptionUnsubscribe | SubscriptionHandle<unknown>) => void,
          reject,
        };
        pendingSubscriptionsByRequest.set(request.requestId, pending);
        pendingSubscriptionsByQuery.set(request.queryId, pending);
        try {
          activeSocket.send(request.frame);
        } catch (error) {
          cleanupPendingSubscription(pending);
          const sendError = toShunterError(error, "transport", "Declared view subscription request send failed");
          handle?.close(sendError);
          reject(sendError);
        }
      });
    }) as DeclaredViewSubscriber<string> & DeclaredViewHandleSubscriber<string>,
    subscribeTable: (async <Table extends string, Row = unknown>(
      table: Table,
      onRows?: (rows: Row[]) => void,
      options: TableSubscriptionOptions<Row> = {},
    ): Promise<SubscriptionUnsubscribe | SubscriptionHandle<Uint8Array> | SubscriptionHandle<Row>> => {
      if (disposed) {
        throw new ShunterClosedClientError("Cannot subscribe a table on a disposed Shunter client.");
      }
      if (options.signal?.aborted) {
        throw new ShunterClosedClientError("Table subscription aborted before sending.");
      }
      const activeSocket = socket;
      if (state.status !== "connected" || activeSocket === undefined) {
        throw new ShunterClosedClientError("Cannot subscribe a table before the Shunter client is connected.");
      }
      const request = encodeTableSubscriptionRequest(table, {
        ...options,
        requestId: options.requestId ?? allocateRequestId(),
        queryId: options.queryId ?? allocateQueryId(),
      });
      if (
        pendingSubscriptionsByRequest.has(request.requestId) ||
        pendingSubscriptionsByQuery.has(request.queryId)
      ) {
        throw new ShunterValidationError("Table subscription ID is already in flight.", {
          details: { table, requestId: request.requestId, queryId: request.queryId },
        });
      }
      const handle = options.returnHandle === true
        ? createSubscriptionHandle<unknown>({
          queryId: request.queryId,
          unsubscribe: tableSubscriptionUnsubscribe(request.queryId),
        })
        : undefined;
      return new Promise<SubscriptionUnsubscribe | SubscriptionHandle<Uint8Array> | SubscriptionHandle<Row>>((resolve, reject) => {
        let cleanup: (() => void) | undefined;
        if (options.signal !== undefined) {
          const abort = (): void => {
            const pending = pendingSubscriptionsByRequest.get(request.requestId);
            if (pending !== undefined) {
              cleanupPendingSubscription(pending);
            }
            cleanup?.();
            const abortError = new ShunterClosedClientError("Table subscription aborted before a response was received.");
            handle?.close(abortError);
            reject(abortError);
          };
          options.signal.addEventListener("abort", abort, { once: true });
          cleanup = () => {
            options.signal?.removeEventListener("abort", abort);
          };
        }
        const pending: PendingSubscription = {
          kind: "table",
          target: table,
          requestId: request.requestId,
          queryId: request.queryId,
          tableName: table,
          onRawRows: options.onRawRows,
          onRawUpdate: options.onRawUpdate,
          onRows: onRows as ((rows: readonly unknown[]) => void) | undefined,
          onInitialRows: options.onInitialRows as ((rows: readonly unknown[]) => void) | undefined,
          onUpdate: options.onUpdate as ((update: SubscriptionUpdate<unknown>) => void) | undefined,
          decodeRow: options.decodeRow as RowDecoder<unknown> | undefined,
          handle,
          cleanup,
          resolve: resolve as (value: SubscriptionUnsubscribe | SubscriptionHandle<unknown>) => void,
          reject,
        };
        pendingSubscriptionsByRequest.set(request.requestId, pending);
        pendingSubscriptionsByQuery.set(request.queryId, pending);
        try {
          activeSocket.send(request.frame);
        } catch (error) {
          cleanupPendingSubscription(pending);
          const sendError = toShunterError(error, "transport", "Table subscription request send failed");
          handle?.close(sendError);
          reject(sendError);
        }
      });
    }) as RawTableSubscriber & RawTableHandleSubscriber & DecodedTableHandleSubscriber,
    close(code = closeNormalCode, reason = ""): Promise<void> {
      return beginClose(code, reason);
    },
    dispose(): Promise<void> {
      disposed = true;
      return beginClose(closeNormalCode, "disposed");
    },
    onStateChange(listener: ConnectionStateListener<Protocol>): () => void {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}

function normalizeReconnectOptions(options: ReconnectOptions | false | undefined): NormalizedReconnectOptions {
  if (options === undefined || options === false || options.enabled !== true) {
    return {
      enabled: false,
      maxAttempts: 0,
      initialDelayMs: 0,
      maxDelayMs: 0,
      backoffMultiplier: 1,
      resubscribe: false,
    };
  }
  return {
    enabled: true,
    maxAttempts: positiveInteger(options.maxAttempts, 3),
    initialDelayMs: nonNegativeNumber(options.initialDelayMs, 250),
    maxDelayMs: nonNegativeNumber(options.maxDelayMs, 5_000),
    backoffMultiplier: Math.max(1, nonNegativeNumber(options.backoffMultiplier, 2)),
    resubscribe: options.resubscribe !== false,
  };
}

function reconnectDelayMs(options: NormalizedReconnectOptions, attempt: number): number {
  return Math.min(
    options.maxDelayMs,
    options.initialDelayMs * (options.backoffMultiplier ** Math.max(0, attempt - 1)),
  );
}

function positiveInteger(value: number | undefined, fallback: number): number {
  if (value === undefined || !Number.isFinite(value)) {
    return fallback;
  }
  return Math.max(0, Math.trunc(value));
}

function nonNegativeNumber(value: number | undefined, fallback: number): number {
  if (value === undefined || !Number.isFinite(value)) {
    return fallback;
  }
  return Math.max(0, value);
}

async function resolveToken(token?: TokenSource): Promise<string | undefined> {
  if (token === undefined) {
    return undefined;
  }
  if (typeof token === "string") {
    return token;
  }
  return token();
}

function withTokenQuery(url: string, token?: string): string {
  if (token === undefined || token === "") {
    return url;
  }
  const parsed = new URL(url);
  parsed.searchParams.set("token", token);
  return parsed.toString();
}

function createWebSocket(
  url: string,
  protocols: readonly ShunterSubprotocol[],
  factory?: WebSocketFactory,
): WebSocketLike {
  if (factory !== undefined) {
    return factory(url, protocols);
  }
  if (typeof WebSocket === "undefined") {
    throw new ShunterTransportError("No WebSocket implementation is available.");
  }
  return new WebSocket(url, [...protocols]);
}

export function decodeIdentityTokenFrame(data: unknown): IdentityTokenMessage {
  const frame = frameBytes(data);
  if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_IDENTITY_TOKEN) {
    throw new ShunterProtocolError("Expected IdentityToken as the first server message.");
  }
  let offset = 1;
  if (frame.length < offset + 32) {
    throw new ShunterProtocolError("Malformed IdentityToken: identity field is truncated.");
  }
  const identity = frame.slice(offset, offset + 32);
  offset += 32;
  const [tokenLength, tokenOffset] = readUint32LE(frame, offset, "IdentityToken token length");
  offset = tokenOffset;
  if (frame.length < offset + tokenLength) {
    throw new ShunterProtocolError("Malformed IdentityToken: token field is truncated.");
  }
  let token: string;
  try {
    token = new TextDecoder("utf-8", { fatal: true }).decode(frame.slice(offset, offset + tokenLength));
  } catch (error) {
    throw new ShunterProtocolError("Malformed IdentityToken: token is not valid UTF-8.", { cause: error });
  }
  offset += tokenLength;
  if (frame.length < offset + 16) {
    throw new ShunterProtocolError("Malformed IdentityToken: connection_id field is truncated.");
  }
  const connectionId = frame.slice(offset, offset + 16);
  offset += 16;
  if (offset !== frame.length) {
    throw new ShunterProtocolError("Malformed IdentityToken: trailing bytes.");
  }
  return { identity, token, connectionId };
}

export type TransactionUpdateStatus =
  | { readonly status: "committed"; readonly updates: readonly RawSubscriptionUpdate[] }
  | { readonly status: "failed"; readonly error: string };

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

export function decodeTransactionUpdateFrame(data: unknown): TransactionUpdateMessage {
  const frame = frameBytes(data);
  if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE) {
    throw new ShunterProtocolError("Expected TransactionUpdate server message.");
  }
  let offset = 1;
  const [status, statusOffset] = readTransactionUpdateStatus(frame, offset);
  offset = statusOffset;
  const [timestamp, timestampOffset] = readInt64LE(frame, offset, "TransactionUpdate timestamp");
  offset = timestampOffset;
  const [callerIdentity, identityOffset] = readFixedBytes(frame, offset, 32, "TransactionUpdate caller_identity");
  offset = identityOffset;
  const [callerConnectionId, connectionOffset] = readFixedBytes(
    frame,
    offset,
    16,
    "TransactionUpdate caller_connection_id",
  );
  offset = connectionOffset;
  const [reducerCall, reducerOffset] = readReducerCallInfo(frame, offset);
  offset = reducerOffset;
  const [totalHostExecutionDuration, durationOffset] = readInt64LE(
    frame,
    offset,
    "TransactionUpdate total_host_execution_duration",
  );
  offset = durationOffset;
  if (offset !== frame.length) {
    throw new ShunterProtocolError("Malformed TransactionUpdate: trailing bytes.");
  }
  return {
    status,
    timestamp,
    callerIdentity,
    callerConnectionId,
    reducerCall,
    totalHostExecutionDuration,
    rawFrame: new Uint8Array(frame),
  };
}

export function decodeTransactionUpdateLightFrame(data: unknown): TransactionUpdateLightMessage {
  const frame = frameBytes(data);
  if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT) {
    throw new ShunterProtocolError("Expected TransactionUpdateLight server message.");
  }
  let offset = 1;
  const [requestId, requestOffset] = readUint32LE(frame, offset, "TransactionUpdateLight request_id");
  offset = requestOffset;
  const [updates, updatesOffset] = readRawSubscriptionUpdates(frame, offset);
  offset = updatesOffset;
  if (offset !== frame.length) {
    throw new ShunterProtocolError("Malformed TransactionUpdateLight: trailing bytes.");
  }
  return {
    requestId,
    updates,
    rawFrame: new Uint8Array(frame),
  };
}

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

export interface DecodedDeclaredQueryResult<
  Name extends string = string,
  RowsByName extends object = Record<string, unknown>,
> {
  readonly name: Name;
  readonly messageId: Uint8Array;
  readonly tables: readonly DecodedDeclaredQueryTableFor<RowsByName>[];
  readonly totalHostExecutionDuration: bigint;
  readonly rawFrame: Uint8Array;
}

export type DeclaredQueryRowDecoder<Row = unknown> = (tableName: string, row: Uint8Array) => Row;

export interface DeclaredQueryDecodeOptions<
  RowsByName extends object = Record<string, unknown>,
> {
  readonly tableDecoders?: TableRowDecoders<RowsByName>;
  readonly decodeRow?: DeclaredQueryRowDecoder<RowsByName[keyof RowsByName & string]>;
}

export function decodeOneOffQueryResponseFrame(data: unknown): OneOffQueryResponseMessage {
  const frame = frameBytes(data);
  if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE) {
    throw new ShunterProtocolError("Expected OneOffQueryResponse server message.");
  }
  let offset = 1;
  const [messageId, messageOffset] = readBytes(frame, offset, "OneOffQueryResponse message_id");
  offset = messageOffset;
  const [error, errorOffset] = readOptionalStringValue(frame, offset, "OneOffQueryResponse error");
  offset = errorOffset;
  const [tables, tablesOffset] = readOneOffQueryTables(frame, offset);
  offset = tablesOffset;
  const [totalHostExecutionDuration, durationOffset] = readInt64LE(
    frame,
    offset,
    "OneOffQueryResponse total_host_execution_duration",
  );
  offset = durationOffset;
  if (offset !== frame.length) {
    throw new ShunterProtocolError("Malformed OneOffQueryResponse: trailing bytes.");
  }
  return {
    messageId,
    ...(error === undefined ? {} : { error }),
    tables,
    totalHostExecutionDuration,
    rawFrame: new Uint8Array(frame),
  };
}

export function decodeRawDeclaredQueryResult<Name extends string>(
  name: Name,
  data: unknown,
): RawDeclaredQueryResult<Name> {
  const response = decodeOneOffQueryResponseFrame(data);
  if (response.error !== undefined) {
    throw new ShunterValidationError(response.error || "Declared query failed.", {
      code: "declared_query_failed",
      details: { name, response },
    });
  }
  return {
    name,
    messageId: new Uint8Array(response.messageId),
    tables: response.tables.map((table) => ({
      tableName: table.tableName,
      rows: new Uint8Array(table.rows),
      rowBytes: table.rowBytes.map((row) => new Uint8Array(row)),
    })),
    totalHostExecutionDuration: response.totalHostExecutionDuration,
    rawFrame: new Uint8Array(response.rawFrame),
  };
}

export function decodeDeclaredQueryResult<
  Name extends string,
  RowsByName extends object = Record<string, unknown>,
>(
  name: Name,
  data: unknown,
  options: DeclaredQueryDecodeOptions<RowsByName>,
): DecodedDeclaredQueryResult<Name, RowsByName> {
  const raw = decodeRawDeclaredQueryResult(name, data);
  const tables = raw.tables.map((table) => {
    const decodeRow = declaredQueryRowDecoderForTable(options, table.tableName);
    if (decodeRow === undefined) {
      throw new ShunterValidationError("No row decoder registered for declared query table.", {
        code: "missing_row_decoder",
        details: { name, tableName: table.tableName },
      });
    }
    return {
      tableName: table.tableName,
      rows: table.rowBytes.map((row, rowIndex) => {
        try {
          return decodeRow(new Uint8Array(row));
        } catch (error) {
          if (isShunterError(error)) {
            throw error;
          }
          throw new ShunterValidationError("Declared query row decoder failed.", {
            cause: error,
            details: { name, tableName: table.tableName, rowIndex },
          });
        }
      }),
      rawRows: new Uint8Array(table.rows),
      rowBytes: table.rowBytes.map((row) => new Uint8Array(row)),
    };
  });
  return {
    name,
    messageId: new Uint8Array(raw.messageId),
    tables: tables as unknown as DecodedDeclaredQueryTableFor<RowsByName>[],
    totalHostExecutionDuration: raw.totalHostExecutionDuration,
    rawFrame: new Uint8Array(raw.rawFrame),
  };
}

function declaredQueryRowDecoderForTable<RowsByName extends object>(
  options: DeclaredQueryDecodeOptions<RowsByName>,
  tableName: string,
): RowDecoder<unknown> | undefined {
  const tableDecoders = options.tableDecoders as
    | Record<string, TableRowDecoder<unknown> | undefined>
    | undefined;
  const tableDecoder = tableDecoders?.[tableName];
  if (tableDecoder !== undefined) {
    return tableDecoder;
  }
  if (options.decodeRow === undefined) {
    return undefined;
  }
  const decodeRow = options.decodeRow;
  return (row: Uint8Array): unknown => decodeRow(tableName, row);
}

export interface RawRowList {
  readonly rows: readonly Uint8Array[];
  readonly rawBytes: Uint8Array;
}

export type BsatnValueKind =
  | "bool"
  | "int8"
  | "uint8"
  | "int16"
  | "uint16"
  | "int32"
  | "uint32"
  | "int64"
  | "uint64"
  | "float32"
  | "float64"
  | "string"
  | "bytes"
  | "int128"
  | "uint128"
  | "int256"
  | "uint256"
  | "timestamp"
  | "arrayString"
  | "uuid"
  | "duration"
  | "json";

export interface BsatnColumn {
  readonly name: string;
  readonly kind: BsatnValueKind;
  readonly nullable?: boolean;
}

const bsatnTags = {
  bool: 0,
  int8: 1,
  uint8: 2,
  int16: 3,
  uint16: 4,
  int32: 5,
  uint32: 6,
  int64: 7,
  uint64: 8,
  float32: 9,
  float64: 10,
  string: 11,
  bytes: 12,
  int128: 13,
  uint128: 14,
  int256: 15,
  uint256: 16,
  timestamp: 17,
  arrayString: 18,
  uuid: 19,
  duration: 20,
  json: 21,
} as const satisfies Record<BsatnValueKind, number>;

export function decodeRowList(data: unknown): RawRowList {
  const rowList = binaryBytes(data, "encoded RowList");
  let offset = 0;
  const [count, countOffset] = readUint32LE(rowList, offset, "RowList row count");
  offset = countOffset;
  if (count > Math.floor((rowList.length - offset) / 4)) {
    throw new ShunterProtocolError("Malformed RowList: row count exceeds remaining bytes.");
  }
  const rows: Uint8Array[] = [];
  for (let i = 0; i < count; i += 1) {
    const [row, rowOffset] = readBytes(rowList, offset, `RowList row ${i}`);
    offset = rowOffset;
    rows.push(row);
  }
  if (offset !== rowList.length) {
    throw new ShunterProtocolError("Malformed RowList: trailing bytes.");
  }
  return {
    rows,
    rawBytes: new Uint8Array(rowList),
  };
}

export function decodeBsatnProduct<Row>(
	data: unknown,
	columns: readonly BsatnColumn[],
	buildRow: (values: readonly unknown[]) => Row,
): Row {
  const row = binaryBytes(data, "encoded BSATN product row");
  let offset = 0;
  const values: unknown[] = [];
  for (const column of columns) {
    const [value, valueOffset] = readBsatnColumn(row, offset, column);
    values.push(value);
    offset = valueOffset;
  }
  if (offset !== row.length) {
    throw new ShunterValidationError("Malformed BSATN row: trailing bytes.", {
      code: "bsatn_trailing_bytes",
      details: { expectedColumns: columns.length },
    });
  }
	return buildRow(values);
}

export function encodeBsatnProduct(
	values: readonly unknown[],
	columns: readonly BsatnColumn[],
): Uint8Array {
	if (values.length !== columns.length) {
		throw new ShunterValidationError("BSATN product value count does not match the column schema.", {
			code: "bsatn_value_count_mismatch",
			details: { expectedColumns: columns.length, receivedValues: values.length },
		});
	}
	const chunks: Uint8Array[] = [];
	for (let i = 0; i < columns.length; i += 1) {
		chunks.push(encodeBsatnColumn(values[i], columns[i]));
	}
	return concatBytes(chunks);
}

function encodeBsatnColumn(value: unknown, column: BsatnColumn): Uint8Array {
	const tag = bsatnTags[column.kind];
	if (column.nullable === true) {
		if (value === null || value === undefined) {
			return new Uint8Array([tag, 0]);
		}
		return concatBytes([new Uint8Array([tag, 1]), encodeBsatnPayload(value, column)]);
	}
	if (value === null || value === undefined) {
		throw new ShunterValidationError("Cannot encode null for a non-nullable BSATN column.", {
			code: "bsatn_non_nullable_null",
			details: { column: column.name },
		});
	}
	return concatBytes([new Uint8Array([tag]), encodeBsatnPayload(value, column)]);
}

function encodeBsatnPayload(value: unknown, column: BsatnColumn): Uint8Array {
	const view8 = new Uint8Array(8);
	const view = new DataView(view8.buffer);
	switch (column.kind) {
		case "bool":
			if (typeof value !== "boolean") {
				throw invalidBsatnValue(column, "boolean");
			}
			return new Uint8Array([value ? 1 : 0]);
		case "int8":
			return new Uint8Array([asInteger(value, column, -0x80, 0x7f) & 0xff]);
		case "uint8":
			return new Uint8Array([asInteger(value, column, 0, 0xff)]);
		case "int16":
			view.setInt16(0, asInteger(value, column, -0x8000, 0x7fff), true);
			return view8.slice(0, 2);
		case "uint16":
			view.setUint16(0, asInteger(value, column, 0, 0xffff), true);
			return view8.slice(0, 2);
		case "int32":
			view.setInt32(0, asInteger(value, column, -0x80000000, 0x7fffffff), true);
			return view8.slice(0, 4);
		case "uint32":
			view.setUint32(0, asInteger(value, column, 0, 0xffffffff), true);
			return view8.slice(0, 4);
		case "int64":
		case "timestamp":
		case "duration":
			view.setBigInt64(
				0,
				asBigIntInRange(value, column, -(1n << 63n), (1n << 63n) - 1n, "64-bit signed integer"),
				true,
			);
			return view8;
		case "uint64":
			view.setBigUint64(
				0,
				asBigIntInRange(value, column, 0n, (1n << 64n) - 1n, "64-bit unsigned integer"),
				true,
			);
			return view8;
		case "float32":
			view.setFloat32(0, asNumber(value, column), true);
			return view8.slice(0, 4);
		case "float64":
			view.setFloat64(0, asNumber(value, column), true);
			return view8;
		case "string":
			return encodeLengthPrefixedBytes(utf8Bytes(asString(value, column), `BSATN ${column.name}`));
		case "bytes":
			return encodeLengthPrefixedBytes(binaryBytes(value, `BSATN ${column.name}`));
		case "int128":
			return encodeWideInteger(value, column, 16, true);
		case "uint128":
			return encodeWideInteger(value, column, 16, false);
		case "int256":
			return encodeWideInteger(value, column, 32, true);
		case "uint256":
			return encodeWideInteger(value, column, 32, false);
		case "arrayString":
			return encodeStringArray(value, column);
		case "uuid":
			return encodeUUID(value, column);
		case "json":
			{
				const json = JSON.stringify(value);
				if (json === undefined) {
					throw invalidBsatnValue(column, "JSON value");
				}
				return encodeLengthPrefixedBytes(utf8Bytes(json, `BSATN ${column.name}`));
			}
	}
}

function encodeLengthPrefixedBytes(bytes: Uint8Array): Uint8Array {
	const out = new Uint8Array(4 + bytes.length);
	writeUint32LE(out, 0, bytes.length);
	out.set(bytes, 4);
	return out;
}

function encodeStringArray(value: unknown, column: BsatnColumn): Uint8Array {
	if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) {
		throw invalidBsatnValue(column, "string[]");
	}
	const encoded = value.map((item) => utf8Bytes(item, `BSATN ${column.name}`));
	const chunks: Uint8Array[] = [new Uint8Array(4)];
	writeUint32LE(chunks[0], 0, encoded.length);
	for (const item of encoded) {
		chunks.push(encodeLengthPrefixedBytes(item));
	}
	return concatBytes(chunks);
}

function encodeUUID(value: unknown, column: BsatnColumn): Uint8Array {
	const text = asString(value, column).replaceAll("-", "");
	if (!/^[0-9a-fA-F]{32}$/.test(text)) {
		throw invalidBsatnValue(column, "UUID string");
	}
	const out = new Uint8Array(16);
	for (let i = 0; i < out.length; i += 1) {
		out[i] = Number.parseInt(text.slice(i * 2, i * 2 + 2), 16);
	}
	return out;
}

function encodeWideInteger(value: unknown, column: BsatnColumn, byteLength: 16 | 32, signed: boolean): Uint8Array {
	const bits = BigInt(byteLength * 8);
	const max = signed ? (1n << (bits - 1n)) - 1n : (1n << bits) - 1n;
	const min = signed ? -(1n << (bits - 1n)) : 0n;
	let n = asBigIntInRange(
		value,
		column,
		min,
		max,
		signed ? `${byteLength * 8}-bit signed integer` : `${byteLength * 8}-bit unsigned integer`,
	);
	if (n < 0) {
		n = (1n << bits) + n;
	}
	const out = new Uint8Array(byteLength);
	for (let i = 0; i < byteLength; i += 1) {
		out[i] = Number((n >> BigInt(i * 8)) & 0xffn);
	}
	return out;
}

function asInteger(value: unknown, column: BsatnColumn, min: number, max: number): number {
	if (typeof value !== "number" || !Number.isInteger(value) || value < min || value > max) {
		throw invalidBsatnValue(column, `integer in [${min}, ${max}]`);
	}
	return value;
}

function asNumber(value: unknown, column: BsatnColumn): number {
	if (typeof value !== "number" || Number.isNaN(value)) {
		throw invalidBsatnValue(column, "number");
	}
	return value;
}

function asBigInt(value: unknown, column: BsatnColumn): bigint {
	if (typeof value === "bigint") {
		return value;
	}
	if (typeof value === "number" && Number.isSafeInteger(value)) {
		return BigInt(value);
	}
	if (typeof value === "string" && /^-?\d+$/.test(value)) {
		return BigInt(value);
	}
	throw invalidBsatnValue(column, "bigint");
}

function asBigIntInRange(
	value: unknown,
	column: BsatnColumn,
	min: bigint,
	max: bigint,
	expected: string,
): bigint {
	const n = asBigInt(value, column);
	if (n < min || n > max) {
		throw invalidBsatnValue(column, expected);
	}
	return n;
}

function asString(value: unknown, column: BsatnColumn): string {
	if (typeof value !== "string") {
		throw invalidBsatnValue(column, "string");
	}
	return value;
}

function invalidBsatnValue(column: BsatnColumn, expected: string): ShunterValidationError {
	return new ShunterValidationError("Value does not match the BSATN column schema.", {
		code: "bsatn_value_type_mismatch",
		details: { column: column.name, kind: column.kind, expected },
	});
}

function concatBytes(chunks: readonly Uint8Array[]): Uint8Array {
	const length = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
	const out = new Uint8Array(length);
	let offset = 0;
	for (const chunk of chunks) {
		out.set(chunk, offset);
		offset += chunk.length;
	}
	return out;
}

function readBsatnColumn(
  row: Uint8Array,
  offset: number,
  column: BsatnColumn,
): [unknown, number] {
  if (row.length < offset + 1) {
    throw new ShunterValidationError("Malformed BSATN row: column tag is truncated.", {
      code: "bsatn_truncated_column",
      details: { column: column.name },
    });
  }
  const expectedTag = bsatnTags[column.kind];
  const tag = row[offset];
  offset += 1;
  if (tag !== expectedTag) {
    throw new ShunterValidationError("Malformed BSATN row: column tag mismatch.", {
      code: "bsatn_column_tag_mismatch",
      details: { column: column.name, expected: column.kind, expectedTag, receivedTag: tag },
    });
  }
  if (column.nullable !== true) {
    return readBsatnPayload(row, offset, column.kind, column.name);
  }
  if (row.length < offset + 1) {
    throw new ShunterValidationError("Malformed BSATN row: nullable presence marker is truncated.", {
      code: "bsatn_truncated_presence",
      details: { column: column.name },
    });
  }
  const presence = row[offset];
  offset += 1;
  switch (presence) {
    case 0:
      return [null, offset];
    case 1:
      return readBsatnPayload(row, offset, column.kind, column.name);
    default:
      throw new ShunterValidationError("Malformed BSATN row: invalid nullable presence marker.", {
        code: "bsatn_invalid_presence",
        details: { column: column.name, presence },
      });
  }
}

function readBsatnPayload(
  row: Uint8Array,
  offset: number,
  kind: BsatnValueKind,
  column: string,
): [unknown, number] {
  const view = new DataView(row.buffer, row.byteOffset, row.byteLength);
  switch (kind) {
    case "bool":
      assertBsatnAvailable(row, offset, 1, column);
      switch (row[offset]) {
        case 0:
          return [false, offset + 1];
        case 1:
          return [true, offset + 1];
        default:
          throw new ShunterValidationError("Malformed BSATN row: invalid bool payload.", {
            code: "bsatn_invalid_bool",
            details: { column, value: row[offset] },
          });
      }
    case "int8":
      assertBsatnAvailable(row, offset, 1, column);
      return [view.getInt8(offset), offset + 1];
    case "uint8":
      assertBsatnAvailable(row, offset, 1, column);
      return [view.getUint8(offset), offset + 1];
    case "int16":
      assertBsatnAvailable(row, offset, 2, column);
      return [view.getInt16(offset, true), offset + 2];
    case "uint16":
      assertBsatnAvailable(row, offset, 2, column);
      return [view.getUint16(offset, true), offset + 2];
    case "int32":
      assertBsatnAvailable(row, offset, 4, column);
      return [view.getInt32(offset, true), offset + 4];
    case "uint32":
      assertBsatnAvailable(row, offset, 4, column);
      return [view.getUint32(offset, true), offset + 4];
    case "int64":
    case "timestamp":
    case "duration":
      assertBsatnAvailable(row, offset, 8, column);
      return [view.getBigInt64(offset, true), offset + 8];
    case "uint64":
      assertBsatnAvailable(row, offset, 8, column);
      return [view.getBigUint64(offset, true), offset + 8];
    case "float32":
      assertBsatnAvailable(row, offset, 4, column);
      return [view.getFloat32(offset, true), offset + 4];
    case "float64":
      assertBsatnAvailable(row, offset, 8, column);
      return [view.getFloat64(offset, true), offset + 8];
    case "string":
      return readBsatnString(row, offset, column);
    case "bytes":
      return readBytes(row, offset, `BSATN ${column}`);
    case "int128":
      return readBsatnWideInt(row, offset, 2, true, column);
    case "uint128":
      return readBsatnWideInt(row, offset, 2, false, column);
    case "int256":
      return readBsatnWideInt(row, offset, 4, true, column);
    case "uint256":
      return readBsatnWideInt(row, offset, 4, false, column);
    case "arrayString":
      return readBsatnStringArray(row, offset, column);
    case "uuid":
      return readBsatnUUID(row, offset, column);
    case "json":
      return readBsatnJSON(row, offset, column);
  }
}

function assertBsatnAvailable(
  row: Uint8Array,
  offset: number,
  length: number,
  column: string,
): void {
  if (row.length < offset + length) {
    throw new ShunterValidationError("Malformed BSATN row: column payload is truncated.", {
      code: "bsatn_truncated_payload",
      details: { column },
    });
  }
}

function readBsatnString(row: Uint8Array, offset: number, column: string): [string, number] {
  const [raw, nextOffset] = readBytes(row, offset, `BSATN ${column}`);
  try {
    return [new TextDecoder("utf-8", { fatal: true }).decode(raw), nextOffset];
  } catch (error) {
    throw new ShunterValidationError("Malformed BSATN row: string payload is not valid UTF-8.", {
      code: "bsatn_invalid_utf8",
      details: { column },
      cause: error,
    });
  }
}

function readBsatnStringArray(
  row: Uint8Array,
  offset: number,
  column: string,
): [readonly string[], number] {
  const [count, countOffset] = readUint32LE(row, offset, `BSATN ${column} array count`);
  offset = countOffset;
  const values: string[] = [];
  for (let i = 0; i < count; i += 1) {
    const [value, valueOffset] = readBsatnString(row, offset, `${column}[${i}]`);
    values.push(value);
    offset = valueOffset;
  }
  return [values, offset];
}

function readBsatnUUID(row: Uint8Array, offset: number, column: string): [string, number] {
  assertBsatnAvailable(row, offset, 16, column);
  const hex = bytesKey(row.slice(offset, offset + 16));
  return [
    `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`,
    offset + 16,
  ];
}

function readBsatnJSON(row: Uint8Array, offset: number, column: string): [unknown, number] {
  const [rawJSON, nextOffset] = readBsatnString(row, offset, column);
  try {
    return [JSON.parse(rawJSON) as unknown, nextOffset];
  } catch (error) {
    throw new ShunterValidationError("Malformed BSATN row: JSON payload is invalid.", {
      code: "bsatn_invalid_json",
      details: { column },
      cause: error,
    });
  }
}

function readBsatnWideInt(
  row: Uint8Array,
  offset: number,
  words: 2 | 4,
  signed: boolean,
  column: string,
): [bigint, number] {
  const length = words * 8;
  assertBsatnAvailable(row, offset, length, column);
  const view = new DataView(row.buffer, row.byteOffset, row.byteLength);
  let value = signed
    ? view.getBigInt64(offset + (words - 1) * 8, true) << BigInt((words - 1) * 64)
    : view.getBigUint64(offset + (words - 1) * 8, true) << BigInt((words - 1) * 64);
  for (let i = 0; i < words - 1; i += 1) {
    value += view.getBigUint64(offset + i * 8, true) << BigInt(i * 64);
  }
  return [value, offset + length];
}

function decodeEnvelopeRowList(rows: Uint8Array): readonly Uint8Array[] {
  return rows.length === 0 ? [] : decodeRowList(rows).rows;
}

function tryDecodeEnvelopeRowList(rows: Uint8Array): readonly Uint8Array[] | undefined {
  try {
    return decodeEnvelopeRowList(rows);
  } catch {
    return undefined;
  }
}

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

export function decodeSubscribeSingleAppliedFrame(data: unknown): SubscribeSingleAppliedMessage {
  const frame = frameBytes(data);
  if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED) {
    throw new ShunterProtocolError("Expected SubscribeSingleApplied server message.");
  }
  let offset = 1;
  const [requestId, requestOffset] = readUint32LE(frame, offset, "SubscribeSingleApplied request_id");
  offset = requestOffset;
  const [totalHostExecutionDurationMicros, durationOffset] = readUint64LE(
    frame,
    offset,
    "SubscribeSingleApplied total_host_execution_duration_micros",
  );
  offset = durationOffset;
  const [queryId, queryOffset] = readUint32LE(frame, offset, "SubscribeSingleApplied query_id");
  offset = queryOffset;
  const [tableName, tableOffset] = readStringValue(frame, offset, "SubscribeSingleApplied table_name");
  offset = tableOffset;
  const [rows, rowsOffset] = readBytes(frame, offset, "SubscribeSingleApplied rows");
  offset = rowsOffset;
  const rowBytes = decodeEnvelopeRowList(rows);
  if (offset !== frame.length) {
    throw new ShunterProtocolError("Malformed SubscribeSingleApplied: trailing bytes.");
  }
  return {
    requestId,
    totalHostExecutionDurationMicros,
    queryId,
    tableName,
    rows,
    rowBytes,
    rawFrame: new Uint8Array(frame),
  };
}

export function decodeUnsubscribeSingleAppliedFrame(data: unknown): UnsubscribeSingleAppliedMessage {
  const frame = frameBytes(data);
  if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED) {
    throw new ShunterProtocolError("Expected UnsubscribeSingleApplied server message.");
  }
  let offset = 1;
  const [requestId, requestOffset] = readUint32LE(frame, offset, "UnsubscribeSingleApplied request_id");
  offset = requestOffset;
  const [totalHostExecutionDurationMicros, durationOffset] = readUint64LE(
    frame,
    offset,
    "UnsubscribeSingleApplied total_host_execution_duration_micros",
  );
  offset = durationOffset;
  const [queryId, queryOffset] = readUint32LE(frame, offset, "UnsubscribeSingleApplied query_id");
  offset = queryOffset;
  const [hasRows, rowsTagOffset] = readBooleanTag(frame, offset, "UnsubscribeSingleApplied has_rows");
  offset = rowsTagOffset;
  let rows: Uint8Array | undefined;
  let rowBytes: readonly Uint8Array[] | undefined;
  if (hasRows) {
    const [decodedRows, rowsOffset] = readBytes(frame, offset, "UnsubscribeSingleApplied rows");
    rows = decodedRows;
    rowBytes = decodeEnvelopeRowList(decodedRows);
    offset = rowsOffset;
  }
  if (offset !== frame.length) {
    throw new ShunterProtocolError("Malformed UnsubscribeSingleApplied: trailing bytes.");
  }
  return {
    requestId,
    totalHostExecutionDurationMicros,
    queryId,
    hasRows,
    ...(rows === undefined ? {} : { rows }),
    ...(rowBytes === undefined ? {} : { rowBytes }),
    rawFrame: new Uint8Array(frame),
  };
}

export function decodeSubscribeMultiAppliedFrame(data: unknown): SubscriptionSetAppliedMessage {
  return decodeSubscriptionSetAppliedFrame(
    data,
    SHUNTER_SERVER_MESSAGE_SUBSCRIBE_MULTI_APPLIED,
    "SubscribeMultiApplied",
  );
}

export function decodeUnsubscribeMultiAppliedFrame(data: unknown): SubscriptionSetAppliedMessage {
  return decodeSubscriptionSetAppliedFrame(
    data,
    SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED,
    "UnsubscribeMultiApplied",
  );
}

export function decodeSubscriptionErrorFrame(data: unknown): SubscriptionErrorMessage {
  const frame = frameBytes(data);
  if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR) {
    throw new ShunterProtocolError("Expected SubscriptionError server message.");
  }
  let offset = 1;
  const [totalHostExecutionDurationMicros, durationOffset] = readUint64LE(
    frame,
    offset,
    "SubscriptionError total_host_execution_duration_micros",
  );
  offset = durationOffset;
  const [requestId, requestOffset] = readOptionalUint32(frame, offset, "SubscriptionError request_id");
  offset = requestOffset;
  const [queryId, queryOffset] = readOptionalUint32(frame, offset, "SubscriptionError query_id");
  offset = queryOffset;
  const [tableId, tableOffset] = readOptionalUint32(frame, offset, "SubscriptionError table_id");
  offset = tableOffset;
  const [error, errorOffset] = readStringValue(frame, offset, "SubscriptionError error");
  offset = errorOffset;
  if (offset !== frame.length) {
    throw new ShunterProtocolError("Malformed SubscriptionError: trailing bytes.");
  }
  return {
    totalHostExecutionDurationMicros,
    ...(requestId === undefined ? {} : { requestId }),
    ...(queryId === undefined ? {} : { queryId }),
    ...(tableId === undefined ? {} : { tableId }),
    error,
    rawFrame: new Uint8Array(frame),
  };
}

function binaryBytes(data: unknown, label: string): Uint8Array {
  if (data instanceof Uint8Array) {
    return data;
  }
  if (data instanceof ArrayBuffer) {
    return new Uint8Array(data);
  }
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
  }
  throw new ShunterProtocolError(`Expected ${label}.`);
}

function frameBytes(data: unknown): Uint8Array {
  return binaryBytes(data, "binary WebSocket frame");
}

function readUint32LE(frame: Uint8Array, offset: number, label: string): [number, number] {
  if (frame.length < offset + 4) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
  }
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
  return [view.getUint32(offset, true), offset + 4];
}

function readInt64LE(frame: Uint8Array, offset: number, label: string): [bigint, number] {
  if (frame.length < offset + 8) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
  }
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
  return [view.getBigInt64(offset, true), offset + 8];
}

function readUint64LE(frame: Uint8Array, offset: number, label: string): [bigint, number] {
  if (frame.length < offset + 8) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
  }
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
  return [view.getBigUint64(offset, true), offset + 8];
}

function readFixedBytes(
  frame: Uint8Array,
  offset: number,
  length: number,
  label: string,
): [Uint8Array, number] {
  if (frame.length < offset + length) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
  }
  return [frame.slice(offset, offset + length), offset + length];
}

function readBytes(frame: Uint8Array, offset: number, label: string): [Uint8Array, number] {
  const [length, bytesOffset] = readUint32LE(frame, offset, `${label} length`);
  if (frame.length < bytesOffset + length) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
  }
  return [frame.slice(bytesOffset, bytesOffset + length), bytesOffset + length];
}

function readStringValue(frame: Uint8Array, offset: number, label: string): [string, number] {
  const [raw, nextOffset] = readBytes(frame, offset, label);
  try {
    return [new TextDecoder("utf-8", { fatal: true }).decode(raw), nextOffset];
  } catch (error) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is not valid UTF-8.`, { cause: error });
  }
}

function readOptionalStringValue(
  frame: Uint8Array,
  offset: number,
  label: string,
): [string | undefined, number] {
  if (frame.length < offset + 1) {
    throw new ShunterProtocolError(`Malformed frame: ${label} option tag is truncated.`);
  }
  const tag = frame[offset];
  offset += 1;
  switch (tag) {
    case 0:
      return [undefined, offset];
    case 1:
      return readStringValue(frame, offset, label);
    default:
      throw new ShunterProtocolError(`Malformed frame: ${label} option tag ${tag}.`);
  }
}

function readOptionalUint32(
  frame: Uint8Array,
  offset: number,
  label: string,
): [number | undefined, number] {
  if (frame.length < offset + 1) {
    throw new ShunterProtocolError(`Malformed frame: ${label} option tag is truncated.`);
  }
  const tag = frame[offset];
  offset += 1;
  switch (tag) {
    case 0:
      return [undefined, offset];
    case 1:
      return readUint32LE(frame, offset, label);
    default:
      throw new ShunterProtocolError(`Malformed frame: ${label} option tag ${tag}.`);
  }
}

function readBooleanTag(frame: Uint8Array, offset: number, label: string): [boolean, number] {
  if (frame.length < offset + 1) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
  }
  switch (frame[offset]) {
    case 0:
      return [false, offset + 1];
    case 1:
      return [true, offset + 1];
    default:
      throw new ShunterProtocolError(`Malformed frame: ${label} tag ${frame[offset]}.`);
  }
}

function readTransactionUpdateStatus(
  frame: Uint8Array,
  offset: number,
): [TransactionUpdateStatus, number] {
  if (frame.length < offset + 1) {
    throw new ShunterProtocolError("Malformed frame: TransactionUpdate status tag is truncated.");
  }
  const tag = frame[offset];
  offset += 1;
  switch (tag) {
    case 0: {
      const [updates, nextOffset] = readRawSubscriptionUpdates(frame, offset);
      return [{ status: "committed", updates }, nextOffset];
    }
    case 1: {
      const [error, nextOffset] = readStringValue(frame, offset, "TransactionUpdate failure error");
      return [{ status: "failed", error }, nextOffset];
    }
    default:
      throw new ShunterProtocolError(`Malformed TransactionUpdate: unknown status tag ${tag}.`);
  }
}

function readRawSubscriptionUpdates(
  frame: Uint8Array,
  offset: number,
): [RawSubscriptionUpdate[], number] {
  const [count, countOffset] = readUint32LE(frame, offset, "SubscriptionUpdate count");
  offset = countOffset;
  if (count > Math.floor((frame.length - offset) / 16)) {
    throw new ShunterProtocolError("Malformed frame: SubscriptionUpdate count exceeds remaining bytes.");
  }
  const updates: RawSubscriptionUpdate[] = [];
  for (let i = 0; i < count; i += 1) {
    const [queryId, queryOffset] = readUint32LE(frame, offset, "SubscriptionUpdate query_id");
    offset = queryOffset;
    const [tableName, tableOffset] = readStringValue(frame, offset, "SubscriptionUpdate table_name");
    offset = tableOffset;
    const [inserts, insertsOffset] = readBytes(frame, offset, "SubscriptionUpdate inserts");
    offset = insertsOffset;
    const [deletes, deletesOffset] = readBytes(frame, offset, "SubscriptionUpdate deletes");
    offset = deletesOffset;
    const insertRowBytes = tryDecodeEnvelopeRowList(inserts);
    const deleteRowBytes = tryDecodeEnvelopeRowList(deletes);
    updates.push({
      queryId,
      tableName,
      inserts,
      deletes,
      ...(insertRowBytes === undefined ? {} : { insertRowBytes }),
      ...(deleteRowBytes === undefined ? {} : { deleteRowBytes }),
    });
  }
  return [updates, offset];
}

function readReducerCallInfo(frame: Uint8Array, offset: number): [ReducerCallInfo, number] {
  const [name, nameOffset] = readStringValue(frame, offset, "ReducerCallInfo reducer_name");
  offset = nameOffset;
  const [reducerId, reducerOffset] = readUint32LE(frame, offset, "ReducerCallInfo reducer_id");
  offset = reducerOffset;
  const [args, argsOffset] = readBytes(frame, offset, "ReducerCallInfo args");
  offset = argsOffset;
  const [requestId, requestOffset] = readUint32LE(frame, offset, "ReducerCallInfo request_id");
  return [{ name, reducerId, args, requestId }, requestOffset];
}

function readOneOffQueryTables(frame: Uint8Array, offset: number): [OneOffQueryTable[], number] {
  const [count, countOffset] = readUint32LE(frame, offset, "OneOffQueryResponse table count");
  offset = countOffset;
  if (count > Math.floor((frame.length - offset) / 8)) {
    throw new ShunterProtocolError("Malformed frame: OneOffQueryResponse table count exceeds remaining bytes.");
  }
  const tables: OneOffQueryTable[] = [];
  for (let i = 0; i < count; i += 1) {
    const [tableName, tableOffset] = readStringValue(frame, offset, "OneOffQueryResponse table_name");
    offset = tableOffset;
    const [rows, rowsOffset] = readBytes(frame, offset, "OneOffQueryResponse rows");
    offset = rowsOffset;
    tables.push({ tableName, rows, rowBytes: decodeEnvelopeRowList(rows) });
  }
  return [tables, offset];
}

function decodeSubscriptionSetAppliedFrame(
  data: unknown,
  expectedTag: number,
  label: string,
): SubscriptionSetAppliedMessage {
  const frame = frameBytes(data);
  if (frame.length < 1 || frame[0] !== expectedTag) {
    throw new ShunterProtocolError(`Expected ${label} server message.`);
  }
  let offset = 1;
  const [requestId, requestOffset] = readUint32LE(frame, offset, `${label} request_id`);
  offset = requestOffset;
  const [totalHostExecutionDurationMicros, durationOffset] = readUint64LE(
    frame,
    offset,
    `${label} total_host_execution_duration_micros`,
  );
  offset = durationOffset;
  const [queryId, queryOffset] = readUint32LE(frame, offset, `${label} query_id`);
  offset = queryOffset;
  const [updates, updatesOffset] = readRawSubscriptionUpdates(frame, offset);
  offset = updatesOffset;
  if (offset !== frame.length) {
    throw new ShunterProtocolError(`Malformed ${label}: trailing bytes.`);
  }
  return {
    requestId,
    totalHostExecutionDurationMicros,
    queryId,
    updates,
    rawFrame: new Uint8Array(frame),
  };
}

function bytesKey(bytes: Uint8Array): string {
  return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

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

export interface EncodedReducerCallOptions<Args = unknown>
  extends ReducerCallOptions, ReducerArgEncodingOptions<Args> {}

export type ReducerCallFlags =
  | typeof SHUNTER_CALL_REDUCER_FLAGS_FULL_UPDATE
  | typeof SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY;

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

export interface EncodedReducerCallResultOptions<Args = unknown, Result = Uint8Array>
  extends ReducerCallResultRequestOptions<Result>, ReducerArgEncodingOptions<Args> {}

export type ReducerCaller<
  Name extends string = string,
  Args = Uint8Array,
  Result = Uint8Array,
> = (name: Name, args: Args, options?: ReducerCallOptions) => Promise<Result>;

export function encodeReducerArgs(args: Uint8Array): Uint8Array;
export function encodeReducerArgs<Args>(args: Args, encodeArgs: ReducerArgEncoder<Args>): Uint8Array;
export function encodeReducerArgs<Args>(
  args: Args | Uint8Array,
  encodeArgs?: ReducerArgEncoder<Args>,
): Uint8Array {
  if (encodeArgs === undefined) {
    if (args instanceof Uint8Array) {
      return new Uint8Array(args);
    }
    throw new ShunterValidationError("Reducer args require an encoder unless they are already Uint8Array.", {
      code: "missing_reducer_arg_encoder",
    });
  }
  const encoded = encodeArgs(args as Args);
  if (!(encoded instanceof Uint8Array)) {
    throw new ShunterValidationError("Reducer arg encoder must return Uint8Array.", {
      code: "invalid_reducer_arg_encoder_result",
    });
  }
  return new Uint8Array(encoded);
}

export function reducerCallOptions<Args>(options: EncodedReducerCallOptions<Args>): ReducerCallOptions {
  return {
    requestId: options.requestId,
    noSuccessNotify: options.noSuccessNotify,
    signal: options.signal,
  };
}

export function reducerCallResultRequestOptions<Args, Result>(
  options: EncodedReducerCallResultOptions<Args, Result>,
): ReducerCallResultRequestOptions<Result> {
  return {
    requestId: options.requestId,
    signal: options.signal,
    decodeResult: options.decodeResult,
  };
}

export async function callReducerWithEncodedArgs<Name extends string, Args>(
  callReducer: ReducerCaller<Name, Uint8Array, Uint8Array>,
  name: Name,
  args: Args,
  options: EncodedReducerCallOptions<Args> = {},
): Promise<Uint8Array> {
  const encodedArgs = options.encodeArgs === undefined
    ? encodeReducerArgs(args as Uint8Array)
    : encodeReducerArgs(args, options.encodeArgs);
  return callReducer(name, encodedArgs, reducerCallOptions(options));
}

export async function callReducerWithResult<Name extends string, Result = Uint8Array>(
  callReducer: ReducerCaller<Name, Uint8Array, Uint8Array>,
  name: Name,
  args: Uint8Array,
  options: ReducerCallResultRequestOptions<Result> = {},
): Promise<ReducerCallResult<Name, Result>> {
  const rawResult = await callReducer(name, args, {
    requestId: options.requestId,
    signal: options.signal,
  });
  return decodeReducerCallResult(name, rawResult, {
    requestId: options.requestId,
    decodeResult: options.decodeResult,
  });
}

export async function callReducerWithEncodedArgsResult<
  Name extends string,
  Args,
  Result = Uint8Array,
>(
  callReducer: ReducerCaller<Name, Uint8Array, Uint8Array>,
  name: Name,
  args: Args,
  options: EncodedReducerCallResultOptions<Args, Result> = {},
): Promise<ReducerCallResult<Name, Result>> {
  const encodedArgs = options.encodeArgs === undefined
    ? encodeReducerArgs(args as Uint8Array)
    : encodeReducerArgs(args, options.encodeArgs);
  return callReducerWithResult(
    callReducer,
    name,
    encodedArgs,
    reducerCallResultRequestOptions(options),
  );
}

export function encodeReducerCallRequest<Name extends string>(
  name: Name,
  args: Uint8Array,
  options: ReducerCallOptions = {},
): EncodedReducerCallRequest<Name> {
  const requestId = options.requestId ?? 0;
  assertUint32(requestId, "Reducer request ID");
  const flags = reducerCallFlags(options);
  const reducerName = utf8Bytes(name, "Reducer name");
  const frameLength =
    1 +
    4 + reducerName.length +
    4 + args.length +
    4 +
    1;
  const frame = new Uint8Array(frameLength);
  let offset = 0;
  frame[offset] = SHUNTER_CLIENT_MESSAGE_CALL_REDUCER;
  offset += 1;
  offset = writeUint32LE(frame, offset, reducerName.length);
  frame.set(reducerName, offset);
  offset += reducerName.length;
  offset = writeUint32LE(frame, offset, args.length);
  frame.set(args, offset);
  offset += args.length;
  offset = writeUint32LE(frame, offset, requestId);
  frame[offset] = flags;
  return {
    name,
    args: new Uint8Array(args),
    requestId,
    flags,
    frame,
  };
}

export function decodeReducerCallResult<Name extends string, Result = Uint8Array>(
  name: Name,
  data: unknown,
  options: ReducerCallResultOptions<Result> = {},
): ReducerCallResult<Name, Result> {
  const update = decodeTransactionUpdateFrame(data) as TransactionUpdateMessage<Name>;
  if (update.reducerCall.name !== name) {
    throw new ShunterProtocolError("Reducer response did not match the expected reducer name.", {
      details: {
        expectedName: name,
        receivedName: update.reducerCall.name,
        requestId: update.reducerCall.requestId,
      },
    });
  }
  if (options.requestId !== undefined && update.reducerCall.requestId !== options.requestId) {
    throw new ShunterProtocolError("Reducer response did not match the expected request ID.", {
      details: {
        name,
        expectedRequestId: options.requestId,
        receivedRequestId: update.reducerCall.requestId,
      },
    });
  }
  const rawResult = new Uint8Array(update.rawFrame);
  if (update.status.status === "failed") {
    return {
      name,
      requestId: update.reducerCall.requestId,
      status: "failed",
      rawResult,
      error: new ShunterValidationError(update.status.error || "Reducer call failed.", {
        code: "reducer_failed",
        details: update,
      }),
    };
  }
  return {
    name,
    requestId: update.reducerCall.requestId,
    status: "committed",
    value: options.decodeResult === undefined
      ? (rawResult as Result)
      : options.decodeResult(update),
    rawResult,
  };
}

function reducerCallFlags(options: ReducerCallOptions): ReducerCallFlags {
  return options.noSuccessNotify === true
    ? SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY
    : SHUNTER_CALL_REDUCER_FLAGS_FULL_UPDATE;
}

function assertUint32(value: number, label: string): void {
  if (!Number.isInteger(value) || value < 0 || value > maxUint32) {
    throw new ShunterValidationError(`${label} must be an unsigned 32-bit integer.`);
  }
}

function utf8Bytes(value: string, label: string): Uint8Array {
  if (!isWellFormedUTF16(value)) {
    throw new ShunterValidationError(`${label} must be valid UTF-8.`);
  }
  return new TextEncoder().encode(value);
}

function isWellFormedUTF16(value: string): boolean {
  for (let i = 0; i < value.length; i += 1) {
    const code = value.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(i + 1);
      if (!Number.isInteger(next) || next < 0xdc00 || next > 0xdfff) {
        return false;
      }
      i += 1;
      continue;
    }
    if (code >= 0xdc00 && code <= 0xdfff) {
      return false;
    }
  }
  return true;
}

function writeUint32LE(frame: Uint8Array, offset: number, value: number): number {
  new DataView(frame.buffer, frame.byteOffset, frame.byteLength).setUint32(offset, value, true);
  return offset + 4;
}

export interface RawQueryOptions {
  readonly requestId?: RequestID;
  readonly signal?: AbortSignal;
}

export type QueryRunner<Result = Uint8Array> = (
  sql: string,
  options?: RawQueryOptions,
) => Promise<Result>;

export interface DeclaredQueryOptions {
  readonly requestId?: RequestID;
  readonly messageId?: Uint8Array;
  readonly signal?: AbortSignal;
}

export interface EncodedDeclaredQueryRequest<Name extends string = string> {
  readonly name: Name;
  readonly requestId?: RequestID;
  readonly messageId: Uint8Array;
  readonly frame: Uint8Array;
}

export interface DeclaredQueryResult<Name extends string = string, Rows = Uint8Array> {
  readonly name: Name;
  readonly requestId: RequestID;
  readonly rows: Rows;
  readonly rawRows?: Uint8Array;
}

export type DeclaredQueryRunner<
  Name extends string = string,
  Result = Uint8Array,
> = (name: Name, options?: DeclaredQueryOptions) => Promise<Result>;

export function encodeDeclaredQueryRequest<Name extends string>(
  name: Name,
  options: DeclaredQueryOptions = {},
): EncodedDeclaredQueryRequest<Name> {
  const requestId = options.requestId;
  if (requestId !== undefined) {
    assertUint32(requestId, "Declared query request ID");
  }
  const messageId = options.messageId === undefined
    ? requestIdMessageId(requestId ?? 0)
    : new Uint8Array(options.messageId);
  const queryName = utf8Bytes(name, "Declared query name");
  const frameLength =
    1 +
    4 + messageId.length +
    4 + queryName.length;
  const frame = new Uint8Array(frameLength);
  let offset = 0;
  frame[offset] = SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY;
  offset += 1;
  offset = writeUint32LE(frame, offset, messageId.length);
  frame.set(messageId, offset);
  offset += messageId.length;
  offset = writeUint32LE(frame, offset, queryName.length);
  frame.set(queryName, offset);
  return {
    name,
    ...(requestId === undefined ? {} : { requestId }),
    messageId,
    frame,
  };
}

function requestIdMessageId(requestId: RequestID): Uint8Array {
  assertUint32(requestId, "Declared query request ID");
  const messageId = new Uint8Array(4);
  writeUint32LE(messageId, 0, requestId);
  return messageId;
}

export interface SubscriptionClosed {
  readonly reason: "unsubscribed" | "closed" | "error";
  readonly error?: ShunterError;
}

export type SubscriptionState<Row = unknown> =
  | { readonly status: "subscribing" }
  | { readonly status: "active"; readonly rows: readonly Row[] }
  | { readonly status: "unsubscribing"; readonly rows: readonly Row[] }
  | { readonly status: "closed"; readonly error?: ShunterError };

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

export function createSubscriptionHandle<Row = unknown>(
  options: SubscriptionHandleOptions<Row> = {},
): ManagedSubscriptionHandle<Row> {
  let state: SubscriptionState<Row> =
    options.initialRows === undefined
      ? { status: "subscribing" }
      : { status: "active", rows: [...options.initialRows] };
  let unsubscribePromise: Promise<void> | undefined;
  let resolveClosed!: (closed: SubscriptionClosed) => void;
  const closed = new Promise<SubscriptionClosed>((resolve) => {
    resolveClosed = resolve;
  });

  const setState = (next: SubscriptionState<Row>): void => {
    state = next;
    options.onStateChange?.(state);
  };

  const finish = (closedState: SubscriptionClosed): void => {
    if (state.status === "closed") {
      return;
    }
    setState(
      closedState.error === undefined
        ? { status: "closed" }
        : { status: "closed", error: closedState.error },
    );
    resolveClosed(closedState);
  };

  return {
    get queryId() {
      return options.queryId;
    },
    get state() {
      return state;
    },
    closed,
    replaceRows(rows: readonly Row[]): void {
      if (state.status === "closed") {
        throw new ShunterClosedClientError("Cannot replace rows on a closed subscription.");
      }
      setState({
        status: state.status === "unsubscribing" ? "unsubscribing" : "active",
        rows: [...rows],
      });
    },
    close(error?: ShunterError): void {
      finish(error === undefined ? { reason: "closed" } : { reason: "error", error });
    },
    async unsubscribe(): Promise<void> {
      if (unsubscribePromise !== undefined) {
        return unsubscribePromise;
      }
      unsubscribePromise = (async () => {
        if (state.status === "closed") {
          return;
        }
        const rows = state.status === "active" || state.status === "unsubscribing" ? state.rows : [];
        setState({ status: "unsubscribing", rows });
        try {
          await options.unsubscribe?.();
          finish({ reason: "unsubscribed" });
        } catch (error) {
          finish({ reason: "error", error: toShunterError(error, "transport", "Unsubscribe failed") });
        }
      })();
      return unsubscribePromise;
    },
  };
}

export interface DeclaredViewSubscriptionOptions<Row = unknown> {
	readonly requestId?: RequestID;
	readonly queryId?: QueryID;
	readonly signal?: AbortSignal;
	readonly returnHandle?: boolean;
	readonly decodeRow?: RowDecoder<Row>;
	readonly onInitialRows?: (rows: readonly Row[]) => void;
	readonly onUpdate?: (update: SubscriptionUpdate<Row>) => void;
	readonly onRawUpdate?: RawSubscriptionUpdateCallback;
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
  readonly frame: Uint8Array;
}

export interface EncodedSubscriptionUnsubscribeRequest {
  readonly requestId: RequestID;
  readonly queryId: QueryID;
  readonly frame: Uint8Array;
}

export interface EncodedUnsubscribeSingleRequest extends EncodedSubscriptionUnsubscribeRequest {}

export interface EncodedUnsubscribeMultiRequest {
  readonly requestId: RequestID;
  readonly queryId: QueryID;
  readonly frame: Uint8Array;
}

export type DeclaredViewSubscriber<Name extends string = string> = <Row = Uint8Array>(
	name: Name,
	options?: DeclaredViewSubscriptionOptions<Row>,
) => Promise<SubscriptionUnsubscribe>;

export type DeclaredViewHandleSubscriber<Name extends string = string> = <Row = Uint8Array>(
	name: Name,
	options: DeclaredViewSubscriptionOptions<Row> & SubscriptionHandleReturnOptions,
) => Promise<SubscriptionHandle<Row>>;

export function encodeSubscribeSingleRequest<Row = unknown>(
  queryString: string,
  options: TableSubscriptionOptions<Row> = {},
): EncodedSubscribeSingleRequest {
  const requestId = options.requestId ?? 0;
  const queryId = options.queryId ?? 0;
  assertUint32(requestId, "SubscribeSingle request ID");
  assertUint32(queryId, "SubscribeSingle query ID");
  const query = utf8Bytes(queryString, "SubscribeSingle query string");
  const frameLength =
    1 +
    4 +
    4 +
    4 + query.length;
  const frame = new Uint8Array(frameLength);
  let offset = 0;
  frame[offset] = SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_SINGLE;
  offset += 1;
  offset = writeUint32LE(frame, offset, requestId);
  offset = writeUint32LE(frame, offset, queryId);
  offset = writeUint32LE(frame, offset, query.length);
  frame.set(query, offset);
  return {
    queryString,
    requestId,
    queryId,
    frame,
  };
}

export function encodeTableSubscriptionRequest<Table extends string, Row = unknown>(
  table: Table,
  options: TableSubscriptionOptions<Row> = {},
): EncodedTableSubscriptionRequest<Table> {
  return {
    table,
    ...encodeSubscribeSingleRequest(`SELECT * FROM ${quoteSqlIdentifier(table)}`, options),
  };
}

export function encodeDeclaredViewSubscriptionRequest<Name extends string>(
  name: Name,
  options: DeclaredViewSubscriptionOptions = {},
): EncodedDeclaredViewSubscriptionRequest<Name> {
  const requestId = options.requestId ?? 0;
  const queryId = options.queryId ?? 0;
  assertUint32(requestId, "Declared view subscription request ID");
  assertUint32(queryId, "Declared view subscription query ID");
  const viewName = utf8Bytes(name, "Declared view name");
  const frameLength =
    1 +
    4 +
    4 +
    4 + viewName.length;
  const frame = new Uint8Array(frameLength);
  let offset = 0;
  frame[offset] = SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW;
  offset += 1;
  offset = writeUint32LE(frame, offset, requestId);
  offset = writeUint32LE(frame, offset, queryId);
  offset = writeUint32LE(frame, offset, viewName.length);
  frame.set(viewName, offset);
  return {
    name,
    requestId,
    queryId,
    frame,
  };
}

export function encodeUnsubscribeSingleRequest(
  queryId: QueryID,
  options: { readonly requestId?: RequestID } = {},
): EncodedUnsubscribeSingleRequest {
  return encodeSubscriptionUnsubscribeRequest(
    SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_SINGLE,
    queryId,
    "UnsubscribeSingle",
    options,
  );
}

export function encodeUnsubscribeMultiRequest(
  queryId: QueryID,
  options: { readonly requestId?: RequestID } = {},
): EncodedUnsubscribeMultiRequest {
  return encodeSubscriptionUnsubscribeRequest(
    SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_MULTI,
    queryId,
    "Unsubscribe",
    options,
  );
}

function encodeSubscriptionUnsubscribeRequest(
  tag: number,
  queryId: QueryID,
  label: string,
  options: { readonly requestId?: RequestID } = {},
): EncodedSubscriptionUnsubscribeRequest {
  const requestId = options.requestId ?? 0;
  assertUint32(requestId, `${label} request ID`);
  assertUint32(queryId, `${label} query ID`);
  const frame = new Uint8Array(1 + 4 + 4);
  let offset = 0;
  frame[offset] = tag;
  offset += 1;
  offset = writeUint32LE(frame, offset, requestId);
  writeUint32LE(frame, offset, queryId);
  return { requestId, queryId, frame };
}

function quoteSqlIdentifier(identifier: string): string {
  return `"${identifier.replaceAll('"', '""')}"`;
}

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

export type TableSubscriber<
  Name extends string = string,
  RowsByName extends Record<Name, unknown> = Record<Name, unknown>,
  Row = never,
> = <Table extends Name>(
  table: Table,
  onRows?: (rows: ([Row] extends [never] ? RowsByName[Table] : Row)[]) => void,
  options?: TableSubscriptionOptions<[Row] extends [never] ? RowsByName[Table] : Row>,
) => Promise<SubscriptionUnsubscribe>;

export type TableHandleSubscriber<Name extends string = string> = <Table extends Name>(
  table: Table,
  onRows: ((rows: Uint8Array[]) => void) | undefined,
  options: TableSubscriptionOptions<Uint8Array> & SubscriptionHandleReturnOptions,
) => Promise<SubscriptionHandle<Uint8Array>>;

export type DecodedTableHandleSubscriber = <Table extends string, Row>(
  table: Table,
  onRows: ((rows: Row[]) => void) | undefined,
  options: TableSubscriptionOptions<Row> & SubscriptionHandleReturnOptions & { readonly decodeRow: RowDecoder<Row> },
) => Promise<SubscriptionHandle<Row>>;

export type RawTableSubscriber = <Table extends string, Row = unknown>(
  table: Table,
  onRows?: (rows: Row[]) => void,
  options?: TableSubscriptionOptions<Row>,
) => Promise<SubscriptionUnsubscribe>;

export type RawTableHandleSubscriber = <Table extends string>(
  table: Table,
  onRows: ((rows: Uint8Array[]) => void) | undefined,
  options: TableSubscriptionOptions<Uint8Array> & SubscriptionHandleReturnOptions,
) => Promise<SubscriptionHandle<Uint8Array>>;

export type ViewSubscriber = (
  sql: string,
  options?: DeclaredViewSubscriptionOptions,
) => Promise<SubscriptionUnsubscribe>;

export interface RuntimeBindings<
  TableName extends string = string,
  RowsByName extends Record<TableName, unknown> = Record<TableName, unknown>,
  ReducerName extends string = string,
  ExecutableQueryName extends string = string,
  ExecutableViewName extends string = string,
> {
  readonly callReducer: ReducerCaller<ReducerName, Uint8Array, Uint8Array>;
  readonly runDeclaredQuery: DeclaredQueryRunner<ExecutableQueryName, Uint8Array>;
  readonly subscribeDeclaredView: DeclaredViewSubscriber<ExecutableViewName>;
  readonly subscribeTable: TableSubscriber<TableName, RowsByName>;
}
