export const SHUNTER_PROTOCOL_V1 = 1 as const;
export const SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_CURRENT_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_SUBPROTOCOL_V1 = "v1.bsatn.shunter" as const;
export const SHUNTER_DEFAULT_SUBPROTOCOL = SHUNTER_SUBPROTOCOL_V1;
export const SHUNTER_SUPPORTED_SUBPROTOCOLS = [SHUNTER_SUBPROTOCOL_V1] as const;
export const SHUNTER_SERVER_MESSAGE_IDENTITY_TOKEN = 1 as const;

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

export interface RuntimeClientOptions<Protocol extends ProtocolMetadata = ProtocolMetadata> {
  readonly url: string;
  readonly protocol: Protocol;
  readonly token?: TokenSource;
  readonly signal?: AbortSignal;
  readonly webSocketFactory?: WebSocketFactory;
  readonly onStateChange?: ConnectionStateListener<Protocol>;
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
  close(code?: number, reason?: string): void;
}

export type WebSocketFactory = (
  url: string,
  protocols: readonly ShunterSubprotocol[],
) => WebSocketLike;

export interface ShunterClient<Protocol extends ProtocolMetadata = ProtocolMetadata> {
  readonly state: ConnectionState<Protocol>;
  connect(): Promise<ConnectionMetadata<Protocol>>;
  close(code?: number, reason?: string): Promise<void>;
  dispose(): Promise<void>;
  onStateChange(listener: ConnectionStateListener<Protocol>): () => void;
}

const closeNormalCode = 1000;

export function createShunterClient<Protocol extends ProtocolMetadata>(
  options: RuntimeClientOptions<Protocol>,
): ShunterClient<Protocol> {
  let state: ConnectionState<Protocol> = { status: "idle" };
  let socket: WebSocketLike | undefined;
  let connectPromise: Promise<ConnectionMetadata<Protocol>> | undefined;
  let closePromise: Promise<void> | undefined;
  let disposed = false;
  let suppressSocketCloseTransition = false;
  let resolveClose: (() => void) | undefined;
  let rejectConnect: ((error: ShunterError) => void) | undefined;
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
    setState({ status: "failed", error });
    rejectConnect?.(error);
    try {
      socket?.close(closeNormalCode, "protocol failure");
    } catch {
      // Closing after a failed opening handshake is best-effort only.
    }
    finishClose();
  };

  const beginClose = (code = closeNormalCode, reason = ""): Promise<void> => {
    if (closePromise !== undefined) {
      return closePromise;
    }
    if (state.status === "closed" || state.status === "failed") {
      finishClose();
      return Promise.resolve();
    }
    if (state.status === "idle") {
      setState({ status: "closed" });
      finishClose();
      return Promise.resolve();
    }
    const closedError = new ShunterClosedClientError("Shunter client connection is closing.");
    rejectConnect?.(closedError);
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

  return {
    get state() {
      return state;
    },
    async connect(): Promise<ConnectionMetadata<Protocol>> {
      if (disposed) {
        throw new ShunterClosedClientError("Cannot connect a disposed Shunter client.");
      }
      if (state.status === "connected") {
        return state.metadata;
      }
      if (connectPromise !== undefined) {
        return connectPromise;
      }

      const attempt = state.status === "reconnecting" ? state.attempt + 1 : 1;
      setState({ status: "connecting", attempt });

      connectPromise = new Promise<ConnectionMetadata<Protocol>>(async (resolve, reject) => {
        rejectConnect = reject;
        let offeredSubprotocol: ShunterSubprotocol;
        let url: string;
        try {
          offeredSubprotocol = selectShunterSubprotocol(options.protocol);
          url = withTokenQuery(options.url, await resolveToken(options.token));
          if (options.signal?.aborted) {
            throw new ShunterClosedClientError("Connection aborted before opening.");
          }
        } catch (error) {
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
        const handlers = {
          open: (): void => {
            try {
              selectedSubprotocol = ws.protocol || offeredSubprotocol;
              assertProtocolCompatible(options.protocol, selectedSubprotocol);
            } catch (error) {
              failConnecting(
                isShunterError(error)
                  ? error
                  : toShunterError(error, "protocol", "Protocol negotiation failed"),
              );
            }
          },
          message: (event: MessageEvent): void => {
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
              setState({ status: "connected", metadata });
              resolve(metadata);
            } catch (error) {
              failConnecting(
                isShunterError(error)
                  ? error
                  : toShunterError(error, "protocol", "Decode IdentityToken failed"),
              );
            }
          },
          close: (event: CloseEvent): void => {
            cleanupSocketListeners(ws, handlers);
            if (suppressSocketCloseTransition) {
              return;
            }
            if (state.status === "connecting") {
              const error = new ShunterTransportError("WebSocket closed before opening.", {
                code: String(event.code),
                details: { reason: event.reason, wasClean: event.wasClean },
              });
              setState({ status: "failed", error });
              reject(error);
            } else if (state.status !== "closed") {
              setState({ status: "closed" });
            }
            finishClose();
          },
          error: (event: Event): void => {
            if (state.status === "connecting") {
              const error = new ShunterTransportError("WebSocket failed before opening.", {
                details: event,
              });
              setState({ status: "failed", error });
              reject(error);
              suppressSocketCloseTransition = true;
              try {
                ws.close(closeNormalCode, "open failed");
              } catch {
                // Nothing useful can be recovered from a failed close here.
              }
              finishClose();
            }
          },
        };
        ws.addEventListener("open", handlers.open);
        ws.addEventListener("message", handlers.message);
        ws.addEventListener("close", handlers.close);
        ws.addEventListener("error", handlers.error);
      });

      return connectPromise;
    },
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

function frameBytes(data: unknown): Uint8Array {
  if (data instanceof Uint8Array) {
    return data;
  }
  if (data instanceof ArrayBuffer) {
    return new Uint8Array(data);
  }
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
  }
  throw new ShunterProtocolError("Expected binary WebSocket frame.");
}

function readUint32LE(frame: Uint8Array, offset: number, label: string): [number, number] {
  if (frame.length < offset + 4) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
  }
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
  return [view.getUint32(offset, true), offset + 4];
}

export type RequestID = number;
export type QueryID = number;
export type TransactionID = number | bigint | string;

export interface ReducerCallOptions {
  readonly requestId?: RequestID;
  readonly noSuccessNotify?: boolean;
  readonly signal?: AbortSignal;
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

export type ReducerCaller<
  Name extends string = string,
  Args = Uint8Array,
  Result = Uint8Array,
> = (name: Name, args: Args, options?: ReducerCallOptions) => Promise<Result>;

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
  readonly signal?: AbortSignal;
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
      setState({ status: "active", rows: [...rows] });
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
  readonly onInitialRows?: (rows: readonly Row[]) => void;
  readonly onUpdate?: (update: SubscriptionUpdate<Row>) => void;
}

export type DeclaredViewSubscriber<Name extends string = string> = (
  name: Name,
  options?: DeclaredViewSubscriptionOptions,
) => Promise<SubscriptionUnsubscribe>;

export interface TableSubscriptionOptions<Row = unknown> {
  readonly requestId?: RequestID;
  readonly queryId?: QueryID;
  readonly signal?: AbortSignal;
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
