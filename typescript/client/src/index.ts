export const SHUNTER_PROTOCOL_V1 = 1 as const;
export const SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_CURRENT_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_SUBPROTOCOL_V1 = "v1.bsatn.shunter" as const;
export const SHUNTER_DEFAULT_SUBPROTOCOL = SHUNTER_SUBPROTOCOL_V1;
export const SHUNTER_SUPPORTED_SUBPROTOCOLS = [SHUNTER_SUBPROTOCOL_V1] as const;
export const SHUNTER_CLIENT_MESSAGE_CALL_REDUCER = 3 as const;
export const SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY = 7 as const;
export const SHUNTER_SERVER_MESSAGE_IDENTITY_TOKEN = 1 as const;
export const SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE = 6 as const;
export const SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE = 5 as const;
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

export interface ShunterClient<Protocol extends ProtocolMetadata = ProtocolMetadata> {
  readonly state: ConnectionState<Protocol>;
  connect(): Promise<ConnectionMetadata<Protocol>>;
  callReducer: ReducerCaller<string, Uint8Array, Uint8Array>;
  runDeclaredQuery: DeclaredQueryRunner<string, Uint8Array>;
  close(code?: number, reason?: string): Promise<void>;
  dispose(): Promise<void>;
  onStateChange(listener: ConnectionStateListener<Protocol>): () => void;
}

const closeNormalCode = 1000;
const maxUint32 = 0xffffffff;

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
  let nextRequestId: RequestID = 1;
  const pendingReducerCalls = new Map<RequestID, PendingReducerCall>();
  const pendingDeclaredQueries = new Map<string, PendingDeclaredQuery>();
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

  const rejectPendingOperations = (error: ShunterError): void => {
    rejectPendingReducerCalls(error);
    rejectPendingDeclaredQueries(error);
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
        case SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE:
          settleReducerResponse(decodeTransactionUpdateFrame(frame));
          return;
        case SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE:
          settleDeclaredQueryResponse(decodeOneOffQueryResponseFrame(frame));
          return;
        default:
          return;
      }
    } catch (error) {
      failConnected(isShunterError(error) ? error : toShunterError(error, "protocol", "Decode server response failed"));
    }
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
              rejectPendingOperations(error);
              reject(error);
            } else if (state.status !== "closed") {
              rejectPendingOperations(new ShunterClosedClientError("Shunter client connection closed.", {
                code: String(event.code),
                details: { reason: event.reason, wasClean: event.wasClean },
              }));
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
            } else if (state.status === "connected") {
              const error = new ShunterTransportError("WebSocket failed.", {
                details: event,
              });
              suppressSocketCloseTransition = true;
              rejectPendingOperations(error);
              setState({ status: "failed", error });
              try {
                ws.close(closeNormalCode, "transport failure");
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

export type TransactionUpdateStatus =
  | { readonly status: "committed"; readonly updates: readonly RawSubscriptionUpdate[] }
  | { readonly status: "failed"; readonly error: string };

export interface RawSubscriptionUpdate {
  readonly queryId: QueryID;
  readonly tableName: string;
  readonly inserts: Uint8Array;
  readonly deletes: Uint8Array;
}

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

export interface OneOffQueryTable {
  readonly tableName: string;
  readonly rows: Uint8Array;
}

export interface OneOffQueryResponseMessage {
  readonly messageId: Uint8Array;
  readonly error?: string;
  readonly tables: readonly OneOffQueryTable[];
  readonly totalHostExecutionDuration: bigint;
  readonly rawFrame: Uint8Array;
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

function readInt64LE(frame: Uint8Array, offset: number, label: string): [bigint, number] {
  if (frame.length < offset + 8) {
    throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
  }
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
  return [view.getBigInt64(offset, true), offset + 8];
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
    updates.push({ queryId, tableName, inserts, deletes });
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
    tables.push({ tableName, rows });
  }
  return [tables, offset];
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

export type ReducerCaller<
  Name extends string = string,
  Args = Uint8Array,
  Result = Uint8Array,
> = (name: Name, args: Args, options?: ReducerCallOptions) => Promise<Result>;

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
