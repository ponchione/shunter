export const SHUNTER_PROTOCOL_V1 = 1 as const;
export const SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_CURRENT_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_SUBPROTOCOL_V1 = "v1.bsatn.shunter" as const;
export const SHUNTER_DEFAULT_SUBPROTOCOL = SHUNTER_SUBPROTOCOL_V1;
export const SHUNTER_SUPPORTED_SUBPROTOCOLS = [SHUNTER_SUBPROTOCOL_V1] as const;

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
