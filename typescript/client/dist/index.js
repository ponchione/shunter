export const SHUNTER_PROTOCOL_V1 = 1;
export const SHUNTER_PROTOCOL_V2 = 2;
export const SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V1;
export const SHUNTER_CURRENT_PROTOCOL_VERSION = SHUNTER_PROTOCOL_V2;
export const SHUNTER_SUBPROTOCOL_V1 = "v1.bsatn.shunter";
export const SHUNTER_SUBPROTOCOL_V2 = "v2.bsatn.shunter";
export const SHUNTER_DEFAULT_SUBPROTOCOL = SHUNTER_SUBPROTOCOL_V2;
export const SHUNTER_SUPPORTED_SUBPROTOCOLS = [SHUNTER_SUBPROTOCOL_V2, SHUNTER_SUBPROTOCOL_V1];
export const SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_SINGLE = 1;
export const SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_SINGLE = 2;
export const SHUNTER_CLIENT_MESSAGE_CALL_REDUCER = 3;
export const SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_MULTI = 6;
export const SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY = 7;
export const SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW = 8;
export const SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY_WITH_PARAMETERS = 9;
export const SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW_WITH_PARAMETERS = 10;
export const SHUNTER_SERVER_MESSAGE_IDENTITY_TOKEN = 1;
export const SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED = 2;
export const SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED = 3;
export const SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR = 4;
export const SHUNTER_SERVER_MESSAGE_ONE_OFF_QUERY_RESPONSE = 6;
export const SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE = 5;
export const SHUNTER_SERVER_MESSAGE_TRANSACTION_UPDATE_LIGHT = 8;
export const SHUNTER_SERVER_MESSAGE_SUBSCRIBE_MULTI_APPLIED = 9;
export const SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED = 10;
export const SHUNTER_CALL_REDUCER_FLAGS_FULL_UPDATE = 0;
export const SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY = 1;
export const SHUNTER_MODULE_CONTRACT_FORMAT = "shunter.module_contract";
export const SHUNTER_MODULE_CONTRACT_VERSION_V1 = 1;
export const SHUNTER_MIN_SUPPORTED_MODULE_CONTRACT_VERSION = SHUNTER_MODULE_CONTRACT_VERSION_V1;
export const SHUNTER_CURRENT_MODULE_CONTRACT_VERSION = SHUNTER_MODULE_CONTRACT_VERSION_V1;
export const shunterProtocol = {
    minSupportedVersion: SHUNTER_MIN_SUPPORTED_PROTOCOL_VERSION,
    currentVersion: SHUNTER_CURRENT_PROTOCOL_VERSION,
    defaultSubprotocol: SHUNTER_DEFAULT_SUBPROTOCOL,
    supportedSubprotocols: SHUNTER_SUPPORTED_SUBPROTOCOLS,
};
export class ShunterError extends Error {
    kind;
    code;
    details;
    cause;
    constructor(kind, message, options = {}) {
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
    constructor(message, options = {}) {
        super("auth", message, options);
    }
}
export class ShunterValidationError extends ShunterError {
    constructor(message, options = {}) {
        super("validation", message, options);
    }
}
export class ShunterProtocolError extends ShunterError {
    constructor(message, options = {}) {
        super("protocol", message, options);
    }
}
export class ShunterProtocolMismatchError extends ShunterError {
    expected;
    receivedVersion;
    receivedSubprotocol;
    constructor(message, options) {
        super("protocol_mismatch", message, options);
        this.expected = options.expected;
        this.receivedVersion = options.receivedVersion;
        this.receivedSubprotocol = options.receivedSubprotocol;
    }
}
export class ShunterTransportError extends ShunterError {
    constructor(message, options = {}) {
        super("transport", message, options);
    }
}
export class ShunterTimeoutError extends ShunterError {
    constructor(message, options = {}) {
        super("timeout", message, options);
    }
}
export class ShunterClosedClientError extends ShunterError {
    constructor(message, options = {}) {
        super("closed", message, options);
    }
}
export function isShunterError(error) {
    return error instanceof ShunterError;
}
export function toShunterError(error, kind = "transport", message = "Shunter operation failed") {
    if (isShunterError(error)) {
        return error;
    }
    if (error instanceof Error) {
        return new ShunterError(kind, error.message || message, { cause: error });
    }
    return new ShunterError(kind, message, { cause: error });
}
export function checkProtocolCompatibility(protocol, selectedSubprotocol) {
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
    if (selectedSubprotocol !== undefined &&
        (!isSupportedShunterSubprotocol(selectedSubprotocol) ||
            !protocol.supportedSubprotocols.includes(selectedSubprotocol))) {
        return {
            ok: false,
            issue: {
                code: "unsupported_selected_subprotocol",
                message: "The server selected an unsupported Shunter WebSocket subprotocol.",
                receivedSubprotocol: selectedSubprotocol,
            },
        };
    }
    if (selectedSubprotocol !== undefined) {
        return { ok: true, subprotocol: selectedSubprotocol };
    }
    const compatibleSubprotocols = compatibleShunterSubprotocols(protocol);
    if (compatibleSubprotocols.length === 0) {
        return {
            ok: false,
            issue: {
                code: "unsupported_default_subprotocol",
                message: "Generated bindings do not support any Shunter WebSocket subprotocol supported by this client runtime.",
                receivedSubprotocol: protocol.defaultSubprotocol,
            },
        };
    }
    return { ok: true, subprotocol: compatibleSubprotocols[0] };
}
export function assertProtocolCompatible(protocol, selectedSubprotocol) {
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
export function selectShunterSubprotocol(protocol) {
    return assertProtocolCompatible(protocol);
}
function selectShunterSubprotocols(protocol) {
    const preferred = assertProtocolCompatible(protocol);
    return [
        preferred,
        ...compatibleShunterSubprotocols(protocol).filter((subprotocol) => subprotocol !== preferred),
    ];
}
function compatibleShunterSubprotocols(protocol) {
    const generatedSubprotocols = protocol.supportedSubprotocols;
    return SHUNTER_SUPPORTED_SUBPROTOCOLS.filter((subprotocol) => generatedSubprotocols.includes(subprotocol));
}
function isSupportedShunterSubprotocol(subprotocol) {
    return SHUNTER_SUPPORTED_SUBPROTOCOLS.includes(subprotocol);
}
function sameProtocolMetadata(left, right) {
    return left.minSupportedVersion === right.minSupportedVersion &&
        left.currentVersion === right.currentVersion &&
        left.defaultSubprotocol === right.defaultSubprotocol &&
        sameStringList(left.supportedSubprotocols, right.supportedSubprotocols);
}
function sameStringList(left, right) {
    if (left.length !== right.length) {
        return false;
    }
    return left.every((value, index) => value === right[index]);
}
export class ShunterContractMismatchError extends ShunterError {
    contract;
    issue;
    constructor(message, options) {
        super("contract_mismatch", message, options);
        this.contract = options.contract;
        this.issue = options.issue;
    }
}
export function checkGeneratedContractCompatibility(contract, options = {}) {
    if (contract.contractFormat !== SHUNTER_MODULE_CONTRACT_FORMAT) {
        return {
            ok: false,
            contract,
            issue: {
                code: "unsupported_contract_format",
                message: "Generated bindings use an unsupported Shunter contract format.",
                receivedFormat: contract.contractFormat,
            },
        };
    }
    if (contract.contractVersion > SHUNTER_CURRENT_MODULE_CONTRACT_VERSION) {
        return {
            ok: false,
            contract,
            issue: {
                code: "generated_contract_too_new",
                message: "Generated bindings require a newer Shunter contract format than this client runtime supports.",
                receivedVersion: contract.contractVersion,
            },
        };
    }
    if (contract.contractVersion < SHUNTER_MIN_SUPPORTED_MODULE_CONTRACT_VERSION) {
        return {
            ok: false,
            contract,
            issue: {
                code: "generated_contract_too_old",
                message: "Generated bindings target an older Shunter contract format than this client runtime supports.",
                receivedVersion: contract.contractVersion,
            },
        };
    }
    if (options.protocol !== undefined && !sameProtocolMetadata(contract.protocol, options.protocol)) {
        return {
            ok: false,
            contract,
            issue: {
                code: "protocol_metadata_mismatch",
                message: "Generated contract metadata does not match the protocol metadata used to create the client.",
            },
        };
    }
    const protocolCompatibility = checkProtocolCompatibility(contract.protocol, options.selectedSubprotocol);
    if (!protocolCompatibility.ok) {
        return {
            ok: false,
            contract,
            issue: {
                code: "protocol_compatibility",
                message: protocolCompatibility.issue.message,
                receivedVersion: protocolCompatibility.issue.receivedVersion,
                receivedFormat: contract.contractFormat,
                protocolIssue: protocolCompatibility.issue,
            },
        };
    }
    if (options.moduleName !== undefined && contract.moduleName !== options.moduleName) {
        return {
            ok: false,
            contract,
            issue: {
                code: "module_name_mismatch",
                message: "Generated bindings are for a different Shunter module.",
                receivedModuleName: contract.moduleName,
            },
        };
    }
    if (options.moduleVersion !== undefined && contract.moduleVersion !== options.moduleVersion) {
        return {
            ok: false,
            contract,
            issue: {
                code: "module_version_mismatch",
                message: "Generated bindings are stale for the expected Shunter module version.",
                receivedModuleVersion: contract.moduleVersion,
            },
        };
    }
    return { ok: true, contract };
}
export function assertGeneratedContractCompatible(contract, options = {}) {
    const result = checkGeneratedContractCompatibility(contract, options);
    if (result.ok) {
        return result.contract;
    }
    throw new ShunterContractMismatchError(result.issue.message, {
        code: result.issue.code,
        details: result.issue,
        contract,
        issue: result.issue,
    });
}
const closeNormalCode = 1000;
const maxUint32 = 0xffffffff;
export function createShunterClient(options) {
    const reconnectOptions = normalizeReconnectOptions(options.reconnect);
    let state = { status: "idle" };
    let socket;
    let connectPromise;
    let closePromise;
    let disposed = false;
    let suppressSocketCloseTransition = false;
    let hasConnected = false;
    let connectGeneration = 0;
    let reconnectAttempt = 0;
    let reconnectTimer;
    let resolveClose;
    let rejectConnect;
    let connectClient;
    let nextRequestId = 1;
    let nextQueryId = 1;
    const pendingReducerCalls = new Map();
    const pendingDeclaredQueries = new Map();
    const pendingSubscriptionsByRequest = new Map();
    const pendingSubscriptionsByQuery = new Map();
    const pendingUnsubscribesByRequest = new Map();
    const pendingUnsubscribesByQuery = new Map();
    const activeSubscriptionsByQuery = new Map();
    const activeSubscriptionAliasesByRootQuery = new Map();
    const listeners = new Set();
    if (options.onStateChange !== undefined) {
        listeners.add(options.onStateChange);
    }
    const setState = (current) => {
        const previous = state;
        state = current;
        const change = { previous, current };
        for (const listener of [...listeners]) {
            listener(change);
        }
    };
    const cleanupSocketListeners = (ws, handlers) => {
        ws.removeEventListener("open", handlers.open);
        ws.removeEventListener("message", handlers.message);
        ws.removeEventListener("close", handlers.close);
        ws.removeEventListener("error", handlers.error);
    };
    const rejectPendingReducerCalls = (error) => {
        for (const [requestId, pending] of pendingReducerCalls) {
            pending.cleanup?.();
            pendingReducerCalls.delete(requestId);
            pending.reject(error);
        }
    };
    const rejectPendingDeclaredQueries = (error) => {
        for (const [messageKey, pending] of pendingDeclaredQueries) {
            pending.cleanup?.();
            pendingDeclaredQueries.delete(messageKey);
            pending.reject(error);
        }
    };
    const cleanupPendingSubscription = (pending) => {
        pending.cleanup?.();
        pendingSubscriptionsByRequest.delete(pending.requestId);
        pendingSubscriptionsByQuery.delete(pending.queryId);
    };
    const rejectPendingSubscriptions = (error) => {
        for (const pending of [...pendingSubscriptionsByRequest.values()]) {
            cleanupPendingSubscription(pending);
            pending.handle?.close(error);
            pending.reject(error);
        }
    };
    const cleanupPendingUnsubscribe = (pending) => {
        pendingUnsubscribesByRequest.delete(pending.requestId);
        pendingUnsubscribesByQuery.delete(pending.queryId);
    };
    const rejectPendingUnsubscribes = (error) => {
        for (const pending of [...pendingUnsubscribesByRequest.values()]) {
            cleanupPendingUnsubscribe(pending);
            removeActiveSubscription(pending.queryId);
            pending.reject(error);
        }
    };
    const rejectInFlightOperations = (error) => {
        rejectPendingReducerCalls(error);
        rejectPendingDeclaredQueries(error);
        rejectPendingSubscriptions(error);
        rejectPendingUnsubscribes(error);
    };
    const closeActiveSubscriptions = (error) => {
        for (const active of new Set(activeSubscriptionsByQuery.values())) {
            active.handle?.close(error);
        }
        activeSubscriptionsByQuery.clear();
        activeSubscriptionAliasesByRootQuery.clear();
    };
    const rejectPendingOperations = (error) => {
        rejectInFlightOperations(error);
        closeActiveSubscriptions(error);
    };
    const clearReconnectTimer = () => {
        if (reconnectTimer === undefined) {
            return;
        }
        clearTimeout(reconnectTimer);
        reconnectTimer = undefined;
    };
    const finishClose = () => {
        socket = undefined;
        connectPromise = undefined;
        closePromise = undefined;
        rejectConnect = undefined;
        resolveClose?.();
        resolveClose = undefined;
    };
    const failConnecting = (error) => {
        suppressSocketCloseTransition = true;
        rejectConnect?.(error);
        setState({ status: "failed", error });
        try {
            socket?.close(closeNormalCode, "protocol failure");
        }
        catch {
            // Closing after a failed opening handshake is best-effort only.
        }
        finishClose();
    };
    const failConnected = (error) => {
        suppressSocketCloseTransition = true;
        rejectPendingOperations(error);
        setState({ status: "failed", error });
        try {
            socket?.close(closeNormalCode, "protocol failure");
        }
        catch {
            // The connection is already failed; close is best-effort.
        }
        finishClose();
    };
    const scheduleReconnect = (error) => {
        if (!reconnectOptions.enabled ||
            !hasConnected ||
            disposed ||
            state.status === "closing" ||
            state.status === "closed") {
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
    const beginClose = (code = closeNormalCode, reason = "") => {
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
        const pendingClose = new Promise((resolve) => {
            resolveClose = resolve;
        });
        closePromise = pendingClose;
        const closingSocket = socket;
        try {
            closingSocket?.close(code, reason);
        }
        catch (error) {
            setState({ status: "closed", error: toShunterError(error, "transport", "WebSocket close failed") });
            finishClose();
        }
        if (closingSocket === undefined) {
            setState({ status: "closed" });
            finishClose();
        }
        return pendingClose;
    };
    const allocateRequestId = () => {
        const requestId = nextRequestId;
        nextRequestId = nextRequestId === maxUint32 ? 1 : nextRequestId + 1;
        return requestId;
    };
    const allocateQueryId = () => {
        const queryId = nextQueryId;
        nextQueryId = nextQueryId === maxUint32 ? 1 : nextQueryId + 1;
        return queryId;
    };
    const allocateReducerRequestId = () => {
        const attempts = Math.min(maxUint32, pendingReducerCalls.size + 1);
        for (let attempt = 0; attempt < attempts; attempt += 1) {
            const requestId = allocateRequestId();
            if (!pendingReducerCalls.has(requestId)) {
                return requestId;
            }
        }
        throw new ShunterValidationError("No reducer request IDs are available.", {
            code: "reducer_request_ids_exhausted",
        });
    };
    const allocateDeclaredQueryRequestId = () => {
        const attempts = Math.min(maxUint32, pendingDeclaredQueries.size + 1);
        for (let attempt = 0; attempt < attempts; attempt += 1) {
            const requestId = allocateRequestId();
            if (!pendingDeclaredQueries.has(bytesKey(requestIdMessageId(requestId)))) {
                return requestId;
            }
        }
        throw new ShunterValidationError("No declared query message IDs are available.", {
            code: "declared_query_message_ids_exhausted",
        });
    };
    const settleReducerResponse = (update) => {
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
    const settleDeclaredQueryResponse = (response) => {
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
    const cloneRawSubscriptionUpdate = (update) => ({
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
    const cloneRowBytes = (rows) => rows.map((row) => new Uint8Array(row));
    const decodeSubscriptionRows = (rows, decodeRow, label) => {
        if (rows === undefined) {
            throw new ShunterProtocolError(`${label} rows were not encoded as a RowList.`);
        }
        return rows.map((row, rowIndex) => {
            try {
                return decodeRow(new Uint8Array(row));
            }
            catch (error) {
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
    const decodeSubscriptionInitialRows = (updates, decodeRow, label) => updates.flatMap((update) => decodeSubscriptionRows(update.insertRowBytes, decodeRow, `${label} initial`));
    const cacheRow = (rowCache, rowBytes, row) => {
        const key = bytesKey(rowBytes);
        const bucket = rowCache.get(key);
        if (bucket === undefined) {
            rowCache.set(key, [row]);
            return;
        }
        bucket.push(row);
    };
    const deleteCachedRow = (rowCache, rowBytes) => {
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
    const cachedRows = (rowCache) => [...rowCache.values()].flat();
    const replaceCachedRows = (active, rowBytes, rows) => {
        if (active.rowCache === undefined) {
            return;
        }
        active.rowCache.clear();
        for (let i = 0; i < rowBytes.length; i += 1) {
            cacheRow(active.rowCache, rowBytes[i], rows[i]);
        }
        active.handle?.replaceRows(cachedRows(active.rowCache));
    };
    const applyCachedUpdate = (active, update, inserts, label) => {
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
        }
        else {
            const decodedInserts = inserts ?? decodeSubscriptionRows(update.insertRowBytes, active.decodeRow, `${label} insert`);
            for (let i = 0; i < update.insertRowBytes.length; i += 1) {
                cacheRow(active.rowCache, update.insertRowBytes[i], decodedInserts[i]);
            }
        }
        active.handle.replaceRows(cachedRows(active.rowCache));
    };
    const registerActiveSubscription = (active, aliases = [active.queryId]) => {
        const previousAliases = activeSubscriptionAliasesByRootQuery.get(active.queryId);
        if (previousAliases !== undefined) {
            for (const alias of previousAliases) {
                activeSubscriptionsByQuery.delete(alias);
            }
        }
        const activeAliases = new Set([active.queryId, ...aliases]);
        activeSubscriptionAliasesByRootQuery.set(active.queryId, activeAliases);
        for (const alias of activeAliases) {
            activeSubscriptionsByQuery.set(alias, active);
        }
    };
    const removeActiveSubscription = (queryId) => {
        const active = activeSubscriptionsByQuery.get(queryId);
        const rootQueryId = active?.queryId ?? queryId;
        const aliases = activeSubscriptionAliasesByRootQuery.get(rootQueryId) ?? new Set([queryId]);
        for (const alias of aliases) {
            activeSubscriptionsByQuery.delete(alias);
        }
        activeSubscriptionAliasesByRootQuery.delete(rootQueryId);
    };
    const dispatchRawSubscriptionUpdates = (updates, label) => {
        for (const update of updates) {
            const active = activeSubscriptionsByQuery.get(update.queryId);
            if (active === undefined) {
                continue;
            }
            if (active.onRawUpdate !== undefined) {
                try {
                    active.onRawUpdate(cloneRawSubscriptionUpdate(update));
                }
                catch (error) {
                    return toShunterError(error, "validation", `${label} raw subscription update callback failed`);
                }
            }
            if (active.onUpdate !== undefined && active.decodeRow !== undefined) {
                let inserts;
                let deletes;
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
                }
                catch (error) {
                    return toShunterError(error, "validation", `${label} subscription update callback failed`);
                }
            }
            else {
                try {
                    applyCachedUpdate(active, update, undefined, label);
                }
                catch (error) {
                    return toShunterError(error, "validation", `${label} subscription cache update failed`);
                }
            }
        }
        return undefined;
    };
    const pendingSubscriptionForResponse = (requestId, queryId, label) => {
        const requestPending = requestId === undefined ? undefined : pendingSubscriptionsByRequest.get(requestId);
        const queryPending = queryId === undefined ? undefined : pendingSubscriptionsByQuery.get(queryId);
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
        if ((requestId !== undefined && requestId !== pending.requestId) ||
            (queryId !== undefined && queryId !== pending.queryId)) {
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
    const pendingUnsubscribeForResponse = (requestId, queryId, label) => {
        const requestPending = requestId === undefined ? undefined : pendingUnsubscribesByRequest.get(requestId);
        const queryPending = queryId === undefined ? undefined : pendingUnsubscribesByQuery.get(queryId);
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
        if ((requestId !== undefined && requestId !== pending.requestId) ||
            (queryId !== undefined && queryId !== pending.queryId)) {
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
    const subscriptionRequestIdInUse = (requestId) => pendingSubscriptionsByRequest.has(requestId) ||
        pendingUnsubscribesByRequest.has(requestId);
    const subscriptionQueryIdInUse = (queryId) => pendingSubscriptionsByQuery.has(queryId) ||
        pendingUnsubscribesByQuery.has(queryId) ||
        activeSubscriptionsByQuery.has(queryId);
    const subscriptionIdInUse = (requestId, queryId) => subscriptionRequestIdInUse(requestId) ||
        subscriptionQueryIdInUse(queryId);
    const subscriptionIdInUseError = (kind, target, requestId, queryId) => new ShunterValidationError("Subscription ID is already in use.", {
        code: "subscription_id_in_use",
        details: { kind, target, requestId, queryId },
    });
    const allocateSubscriptionRequestId = () => {
        const attempts = Math.min(maxUint32, pendingSubscriptionsByRequest.size + pendingUnsubscribesByRequest.size + 1);
        for (let attempt = 0; attempt < attempts; attempt += 1) {
            const requestId = allocateRequestId();
            if (!subscriptionRequestIdInUse(requestId)) {
                return requestId;
            }
        }
        throw new ShunterValidationError("No subscription request IDs are available.", {
            code: "subscription_request_ids_exhausted",
        });
    };
    const allocateSubscriptionQueryId = () => {
        const attempts = Math.min(maxUint32, pendingSubscriptionsByQuery.size + pendingUnsubscribesByQuery.size + activeSubscriptionsByQuery.size + 1);
        for (let attempt = 0; attempt < attempts; attempt += 1) {
            const queryId = allocateQueryId();
            if (!subscriptionQueryIdInUse(queryId)) {
                return queryId;
            }
        }
        throw new ShunterValidationError("No subscription query IDs are available.", {
            code: "subscription_query_ids_exhausted",
        });
    };
    const unsubscribeOnce = (kind, queryId, encodeRequest, closedMessage, sendMessage) => {
        let unsubscribePromise;
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
                    requestId: allocateSubscriptionRequestId(),
                });
                if (pendingUnsubscribesByRequest.has(request.requestId) ||
                    pendingUnsubscribesByQuery.has(request.queryId)) {
                    throw new ShunterValidationError("Unsubscribe request ID is already in flight.", {
                        details: { kind, requestId: request.requestId, queryId: request.queryId },
                    });
                }
                await new Promise((resolve, reject) => {
                    const pendingUnsubscribe = {
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
                    }
                    catch (error) {
                        cleanupPendingUnsubscribe(pendingUnsubscribe);
                        reject(toShunterError(error, "transport", sendMessage));
                    }
                });
            })();
            return unsubscribePromise;
        };
    };
    const declaredViewUnsubscribe = (queryId) => {
        let unsubscribe;
        return () => {
            if (unsubscribe === undefined) {
                const active = activeSubscriptionsByQuery.get(queryId);
                const encodeUnsubscribe = active?.unsubscribeMode === "single"
                    ? encodeUnsubscribeSingleRequest
                    : encodeUnsubscribeMultiRequest;
                unsubscribe = unsubscribeOnce("declared_view", queryId, encodeUnsubscribe, "Cannot unsubscribe after the Shunter client is disconnected.", "Unsubscribe request send failed");
            }
            return unsubscribe();
        };
    };
    const tableSubscriptionUnsubscribe = (queryId) => unsubscribeOnce("table", queryId, encodeUnsubscribeSingleRequest, "Cannot unsubscribe a table subscription after the Shunter client is disconnected.", "Table unsubscribe request send failed");
    const resubscribeActiveSubscriptions = (activeSocket) => {
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
                    params: active.params,
                });
            const requestParams = "params" in request && request.params instanceof Uint8Array
                ? request.params
                : undefined;
            if (requestParams !== undefined) {
                try {
                    assertDeclaredReadParametersSupported(state.status === "connected" ? state.metadata.subprotocol : undefined, options.protocol);
                }
                catch (error) {
                    return isShunterError(error) ? error : toShunterError(error, "protocol_mismatch", "Declared view resubscribe protocol check failed");
                }
            }
            if (pendingSubscriptionsByRequest.has(request.requestId) ||
                pendingSubscriptionsByQuery.has(request.queryId)) {
                return new ShunterValidationError("Reconnect subscription ID is already in flight.", {
                    details: {
                        kind: active.kind,
                        target: active.target,
                        requestId: request.requestId,
                        queryId: request.queryId,
                    },
                });
            }
            const pending = {
                kind: active.kind,
                target: active.target,
                requestId: request.requestId,
                queryId: request.queryId,
                tableName: active.tableName,
                params: requestParams,
                onRawRows: active.onRawRows,
                onRawUpdate: active.onRawUpdate,
                onRows: active.onRows,
                onInitialRows: active.onInitialRows,
                onUpdate: active.onUpdate,
                decodeRow: active.decodeRow,
                handle: active.handle,
                resolve: () => { },
                reject: (error) => {
                    removeActiveSubscription(active.queryId);
                    active.handle?.close(error);
                },
            };
            pendingSubscriptionsByRequest.set(request.requestId, pending);
            pendingSubscriptionsByQuery.set(request.queryId, pending);
            try {
                activeSocket.send(request.frame);
            }
            catch (error) {
                cleanupPendingSubscription(pending);
                return toShunterError(error, "transport", "Reconnect subscription request send failed");
            }
        }
        return undefined;
    };
    const settleTableSubscriptionApplied = (response) => {
        const pending = pendingSubscriptionForResponse(response.requestId, response.queryId, "SubscribeSingleApplied");
        if (pending === undefined) {
            return;
        }
        if ((pending.kind !== "table" && pending.kind !== "declared_view") ||
            (pending.kind === "table" && pending.tableName !== response.tableName)) {
            failConnected(new ShunterProtocolError("SubscribeSingleApplied response did not match the pending table or declared view subscription.", {
                details: {
                    expectedKind: pending.kind,
                    expectedTableName: pending.tableName,
                    receivedTableName: response.tableName,
                    response,
                },
            }));
            return;
        }
        const active = {
            kind: pending.kind,
            target: pending.target,
            queryId: response.queryId,
            tableName: response.tableName,
            params: pending.params,
            unsubscribeMode: "single",
            onRawRows: pending.onRawRows,
            onRawUpdate: pending.onRawUpdate,
            onRows: pending.onRows,
            onInitialRows: pending.onInitialRows,
            onUpdate: pending.onUpdate,
            decodeRow: pending.decodeRow,
            handle: pending.handle,
            rowCache: pending.handle === undefined ? undefined : new Map(),
        };
        registerActiveSubscription(active);
        if (pending.onRawRows !== undefined) {
            try {
                pending.onRawRows({
                    ...response,
                    rows: new Uint8Array(response.rows),
                    rowBytes: cloneRowBytes(response.rowBytes),
                    rawFrame: new Uint8Array(response.rawFrame),
                });
            }
            catch (error) {
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
            }
            catch (error) {
                const callbackError = toShunterError(error, "validation", "SubscribeSingleApplied row callback failed");
                removeActiveSubscription(response.queryId);
                cleanupPendingSubscription(pending);
                pending.handle?.close(callbackError);
                pending.reject(callbackError);
                failConnected(callbackError);
                return;
            }
        }
        else {
            try {
                const rows = cloneRowBytes(response.rowBytes);
                replaceCachedRows(active, response.rowBytes, rows);
                pending.onRows?.(cloneRowBytes(rows));
                pending.onInitialRows?.(cloneRowBytes(rows));
            }
            catch (error) {
                const callbackError = toShunterError(error, "validation", "SubscribeSingleApplied raw row callback failed");
                removeActiveSubscription(response.queryId);
                cleanupPendingSubscription(pending);
                pending.handle?.close(callbackError);
                pending.reject(callbackError);
                failConnected(callbackError);
                return;
            }
        }
        cleanupPendingSubscription(pending);
        pending.resolve(pending.handle ?? (pending.kind === "declared_view"
            ? declaredViewUnsubscribe(response.queryId)
            : tableSubscriptionUnsubscribe(response.queryId)));
    };
    const settleDeclaredViewSubscriptionApplied = (response) => {
        const pending = pendingSubscriptionForResponse(response.requestId, response.queryId, "SubscribeMultiApplied");
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
            params: pending.params,
            unsubscribeMode: "multi",
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
            }
            catch (error) {
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
    const settleSubscriptionError = (response) => {
        const pending = pendingSubscriptionForResponse(response.requestId, response.queryId, "SubscriptionError");
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
    const settleUnsubscribeError = (response) => {
        const pending = pendingUnsubscribeForResponse(response.requestId, response.queryId, "SubscriptionError");
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
    const settleActiveSubscriptionError = (response) => {
        if (response.queryId === undefined) {
            return;
        }
        const active = activeSubscriptionsByQuery.get(response.queryId);
        if (active === undefined) {
            return;
        }
        failConnected(new ShunterValidationError(response.error || "Subscription failed.", {
            code: "subscription_failed",
            details: { kind: active.kind, target: active.target, response },
        }));
    };
    const settleUnsubscribeApplied = (response, allowedKinds, label) => {
        const pending = pendingUnsubscribeForResponse(response.requestId, response.queryId, label);
        if (pending === undefined) {
            return;
        }
        if (!allowedKinds.includes(pending.kind)) {
            failConnected(new ShunterProtocolError(`${label} response did not match the pending unsubscribe kind.`, {
                details: { expectedKind: pending.kind, allowedKinds, response },
            }));
            return;
        }
        cleanupPendingUnsubscribe(pending);
        removeActiveSubscription(response.queryId);
        pending.resolve();
    };
    const settleTransactionUpdateLight = (update) => {
        const updateError = dispatchRawSubscriptionUpdates(update.updates, "TransactionUpdateLight");
        if (updateError !== undefined) {
            failConnected(updateError);
        }
    };
    const handleConnectedMessage = (event) => {
        let frame;
        try {
            frame = frameBytes(event.data);
        }
        catch (error) {
            failConnected(isShunterError(error) ? error : toShunterError(error, "protocol", "Decode server frame failed"));
            return;
        }
        if (frame.length === 0) {
            failConnected(new ShunterProtocolError("Server frame did not include a message tag.", {
                code: "missing_server_message_tag",
            }));
            return;
        }
        try {
            switch (frame[0]) {
                case SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED:
                    settleTableSubscriptionApplied(decodeSubscribeSingleAppliedFrame(frame));
                    return;
                case SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED:
                    settleUnsubscribeApplied(decodeUnsubscribeSingleAppliedFrame(frame), ["table", "declared_view"], "UnsubscribeSingleApplied");
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
                        if (response.requestId === undefined && response.queryId === undefined) {
                            failConnected(new ShunterValidationError(response.error || "Subscription evaluation failed.", {
                                code: "subscription_evaluation_failed",
                                details: response,
                            }));
                            return;
                        }
                        settleSubscriptionError(response);
                        settleUnsubscribeError(response);
                        settleActiveSubscriptionError(response);
                    }
                    return;
                case SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED:
                    settleUnsubscribeApplied(decodeUnsubscribeMultiAppliedFrame(frame), ["declared_view"], "UnsubscribeMultiApplied");
                    return;
                default:
                    failConnected(new ShunterProtocolError("Server sent an unsupported Shunter message.", {
                        code: "unsupported_server_message",
                        details: { tag: frame[0] },
                    }));
                    return;
            }
        }
        catch (error) {
            failConnected(isShunterError(error) ? error : toShunterError(error, "protocol", "Decode server response failed"));
        }
    };
    connectClient = async () => {
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
        connectPromise = new Promise(async (resolve, reject) => {
            rejectConnect = reject;
            let offeredSubprotocols;
            let url;
            let tokenAwaitStarted = false;
            try {
                if (options.contract !== undefined) {
                    assertGeneratedContractCompatible(options.contract, { protocol: options.protocol });
                }
                offeredSubprotocols = selectShunterSubprotocols(options.protocol);
                tokenAwaitStarted = true;
                const token = await resolveToken(options.token);
                if (connectGeneration !== generation || state.status !== "connecting") {
                    return;
                }
                url = withTokenQuery(options.url, token);
                if (options.signal?.aborted) {
                    throw new ShunterClosedClientError("Connection aborted before opening.");
                }
            }
            catch (error) {
                if (tokenAwaitStarted && (connectGeneration !== generation || state.status !== "connecting")) {
                    return;
                }
                const shunterError = isShunterError(error)
                    ? error
                    : new ShunterAuthError("Token provider failed.", { cause: error });
                setState({ status: "failed", error: shunterError });
                finishClose();
                reject(shunterError);
                return;
            }
            let ws;
            try {
                ws = createWebSocket(url, offeredSubprotocols, options.webSocketFactory);
            }
            catch (error) {
                const shunterError = toShunterError(error, "transport", "Create WebSocket failed");
                setState({ status: "failed", error: shunterError });
                finishClose();
                reject(shunterError);
                return;
            }
            socket = ws;
            suppressSocketCloseTransition = false;
            ws.binaryType = "arraybuffer";
            let selectedSubprotocol;
            let cleanupConnectAbort;
            const cleanupOpeningAbort = () => {
                cleanupConnectAbort?.();
                cleanupConnectAbort = undefined;
            };
            const handlers = {
                open: () => {
                    if (socket !== ws) {
                        return;
                    }
                    try {
                        selectedSubprotocol = ws.protocol;
                        assertProtocolCompatible(options.protocol, selectedSubprotocol);
                    }
                    catch (error) {
                        cleanupOpeningAbort();
                        failConnecting(isShunterError(error)
                            ? error
                            : toShunterError(error, "protocol", "Protocol negotiation failed"));
                    }
                },
                message: (event) => {
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
                        const metadata = {
                            protocol: options.protocol,
                            subprotocol: selectedSubprotocol ?? ws.protocol ?? offeredSubprotocols[0],
                            identityToken: identityToken.token,
                            identity: identityToken.identity,
                            connectionId: identityToken.connectionId,
                            contract: options.contract,
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
                    }
                    catch (error) {
                        cleanupOpeningAbort();
                        failConnecting(isShunterError(error)
                            ? error
                            : toShunterError(error, "protocol", "Decode IdentityToken failed"));
                    }
                },
                close: (event) => {
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
                    }
                    else if (state.status === "closing") {
                        setState({ status: "closed" });
                        finishClose();
                    }
                    else if (state.status !== "closed") {
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
                error: (event) => {
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
                        }
                        catch {
                            // Nothing useful can be recovered from a failed close here.
                        }
                    }
                    else if (state.status === "connected") {
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
                        }
                        catch {
                            // Nothing useful can be recovered from a failed close here.
                        }
                    }
                },
            };
            const abortOpening = () => {
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
                }
                catch {
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
        async callReducer(name, args, options = {}) {
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
                requestId: options.requestId ?? allocateReducerRequestId(),
            });
            if (pendingReducerCalls.has(request.requestId)) {
                throw new ShunterValidationError("Reducer request ID is already in flight.", {
                    code: "reducer_request_id_in_use",
                    details: { requestId: request.requestId },
                });
            }
            if (request.flags === SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY) {
                try {
                    activeSocket.send(request.frame);
                }
                catch (error) {
                    throw toShunterError(error, "transport", "Reducer request send failed");
                }
                return request.frame;
            }
            return new Promise((resolve, reject) => {
                let cleanup;
                if (options.signal !== undefined) {
                    const abort = () => {
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
                }
                catch (error) {
                    pendingReducerCalls.delete(request.requestId);
                    cleanup?.();
                    reject(toShunterError(error, "transport", "Reducer request send failed"));
                }
            });
        },
        async runDeclaredQuery(name, options = {}) {
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
                requestId: options.messageId === undefined
                    ? options.requestId ?? allocateDeclaredQueryRequestId()
                    : options.requestId,
            });
            if (request.params !== undefined) {
                assertDeclaredReadParametersSupported(state.metadata.subprotocol, state.metadata.protocol);
            }
            const messageKey = bytesKey(request.messageId);
            if (pendingDeclaredQueries.has(messageKey)) {
                throw new ShunterValidationError("Declared query message ID is already in flight.", {
                    code: "declared_query_message_id_in_use",
                    details: { name, messageId: request.messageId },
                });
            }
            return new Promise((resolve, reject) => {
                let cleanup;
                if (options.signal !== undefined) {
                    const abort = () => {
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
                }
                catch (error) {
                    pendingDeclaredQueries.delete(messageKey);
                    cleanup?.();
                    reject(toShunterError(error, "transport", "Declared query request send failed"));
                }
            });
        },
        subscribeDeclaredView: (async (name, options = {}) => {
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
                requestId: options.requestId ?? allocateSubscriptionRequestId(),
                queryId: options.queryId ?? allocateSubscriptionQueryId(),
            });
            if (request.params !== undefined) {
                assertDeclaredReadParametersSupported(state.metadata.subprotocol, state.metadata.protocol);
            }
            if (subscriptionIdInUse(request.requestId, request.queryId)) {
                throw subscriptionIdInUseError("declared_view", name, request.requestId, request.queryId);
            }
            const handle = options.returnHandle === true
                ? createSubscriptionHandle({
                    queryId: request.queryId,
                    unsubscribe: declaredViewUnsubscribe(request.queryId),
                })
                : undefined;
            return new Promise((resolve, reject) => {
                let cleanup;
                if (options.signal !== undefined) {
                    const abort = () => {
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
                const pending = {
                    kind: "declared_view",
                    target: name,
                    requestId: request.requestId,
                    queryId: request.queryId,
                    params: request.params,
                    onInitialRows: options.onInitialRows,
                    onUpdate: options.onUpdate,
                    onRawUpdate: options.onRawUpdate,
                    decodeRow: options.decodeRow,
                    handle,
                    cleanup,
                    resolve: resolve,
                    reject,
                };
                pendingSubscriptionsByRequest.set(request.requestId, pending);
                pendingSubscriptionsByQuery.set(request.queryId, pending);
                try {
                    activeSocket.send(request.frame);
                }
                catch (error) {
                    cleanupPendingSubscription(pending);
                    const sendError = toShunterError(error, "transport", "Declared view subscription request send failed");
                    handle?.close(sendError);
                    reject(sendError);
                }
            });
        }),
        subscribeTable: (async (table, onRows, options = {}) => {
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
                requestId: options.requestId ?? allocateSubscriptionRequestId(),
                queryId: options.queryId ?? allocateSubscriptionQueryId(),
            });
            if (subscriptionIdInUse(request.requestId, request.queryId)) {
                throw subscriptionIdInUseError("table", table, request.requestId, request.queryId);
            }
            const handle = options.returnHandle === true
                ? createSubscriptionHandle({
                    queryId: request.queryId,
                    unsubscribe: tableSubscriptionUnsubscribe(request.queryId),
                })
                : undefined;
            return new Promise((resolve, reject) => {
                let cleanup;
                if (options.signal !== undefined) {
                    const abort = () => {
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
                const pending = {
                    kind: "table",
                    target: table,
                    requestId: request.requestId,
                    queryId: request.queryId,
                    tableName: table,
                    onRawRows: options.onRawRows,
                    onRawUpdate: options.onRawUpdate,
                    onRows: onRows,
                    onInitialRows: options.onInitialRows,
                    onUpdate: options.onUpdate,
                    decodeRow: options.decodeRow,
                    handle,
                    cleanup,
                    resolve: resolve,
                    reject,
                };
                pendingSubscriptionsByRequest.set(request.requestId, pending);
                pendingSubscriptionsByQuery.set(request.queryId, pending);
                try {
                    activeSocket.send(request.frame);
                }
                catch (error) {
                    cleanupPendingSubscription(pending);
                    const sendError = toShunterError(error, "transport", "Table subscription request send failed");
                    handle?.close(sendError);
                    reject(sendError);
                }
            });
        }),
        close(code = closeNormalCode, reason = "") {
            return beginClose(code, reason);
        },
        dispose() {
            disposed = true;
            return beginClose(closeNormalCode, "disposed");
        },
        onStateChange(listener) {
            listeners.add(listener);
            return () => {
                listeners.delete(listener);
            };
        },
    };
}
function normalizeReconnectOptions(options) {
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
function reconnectDelayMs(options, attempt) {
    return Math.min(options.maxDelayMs, options.initialDelayMs * (options.backoffMultiplier ** Math.max(0, attempt - 1)));
}
function positiveInteger(value, fallback) {
    if (value === undefined || !Number.isFinite(value)) {
        return fallback;
    }
    return Math.max(0, Math.trunc(value));
}
function nonNegativeNumber(value, fallback) {
    if (value === undefined || !Number.isFinite(value)) {
        return fallback;
    }
    return Math.max(0, value);
}
async function resolveToken(token) {
    if (token === undefined) {
        return undefined;
    }
    if (typeof token === "string") {
        return token;
    }
    const resolved = await token();
    if (typeof resolved !== "string") {
        throw new ShunterAuthError("Token provider failed.", {
            code: "invalid_token_provider_result",
            details: { receivedType: typeof resolved },
        });
    }
    return resolved;
}
function withTokenQuery(url, token) {
    if (token === undefined || token === "") {
        return url;
    }
    const parsed = new URL(url);
    parsed.searchParams.set("token", token);
    return parsed.toString();
}
function createWebSocket(url, protocols, factory) {
    if (factory !== undefined) {
        return factory(url, protocols);
    }
    if (typeof WebSocket === "undefined") {
        throw new ShunterTransportError("No WebSocket implementation is available.");
    }
    return new WebSocket(url, [...protocols]);
}
export function decodeIdentityTokenFrame(data) {
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
    let token;
    try {
        token = new TextDecoder("utf-8", { fatal: true }).decode(frame.slice(offset, offset + tokenLength));
    }
    catch (error) {
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
export function decodeTransactionUpdateFrame(data) {
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
    const [callerConnectionId, connectionOffset] = readFixedBytes(frame, offset, 16, "TransactionUpdate caller_connection_id");
    offset = connectionOffset;
    const [reducerCall, reducerOffset] = readReducerCallInfo(frame, offset);
    offset = reducerOffset;
    const [totalHostExecutionDuration, durationOffset] = readInt64LE(frame, offset, "TransactionUpdate total_host_execution_duration");
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
export function decodeTransactionUpdateLightFrame(data) {
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
export function decodeOneOffQueryResponseFrame(data) {
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
    const [totalHostExecutionDuration, durationOffset] = readInt64LE(frame, offset, "OneOffQueryResponse total_host_execution_duration");
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
export function decodeRawDeclaredQueryResult(name, data) {
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
export function decodeDeclaredQueryResult(name, data, options) {
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
                }
                catch (error) {
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
        tables: tables,
        totalHostExecutionDuration: raw.totalHostExecutionDuration,
        rawFrame: new Uint8Array(raw.rawFrame),
    };
}
function declaredQueryRowDecoderForTable(options, tableName) {
    const tableDecoders = options.tableDecoders;
    const tableDecoder = tableDecoders?.[tableName];
    if (tableDecoder !== undefined) {
        return tableDecoder;
    }
    if (options.decodeRow === undefined) {
        return undefined;
    }
    const decodeRow = options.decodeRow;
    return (row) => decodeRow(tableName, row);
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
};
export function decodeRowList(data) {
    const rowList = binaryBytes(data, "encoded RowList");
    let offset = 0;
    const [count, countOffset] = readUint32LE(rowList, offset, "RowList row count");
    offset = countOffset;
    if (count > Math.floor((rowList.length - offset) / 4)) {
        throw new ShunterProtocolError("Malformed RowList: row count exceeds remaining bytes.");
    }
    const rows = [];
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
export function decodeBsatnProduct(data, columns, buildRow) {
    const row = binaryBytes(data, "encoded BSATN product row");
    let offset = 0;
    const values = [];
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
export function encodeBsatnProduct(values, columns) {
    if (values.length !== columns.length) {
        throw new ShunterValidationError("BSATN product value count does not match the column schema.", {
            code: "bsatn_value_count_mismatch",
            details: { expectedColumns: columns.length, receivedValues: values.length },
        });
    }
    const chunks = [];
    for (let i = 0; i < columns.length; i += 1) {
        chunks.push(encodeBsatnColumn(values[i], columns[i]));
    }
    return concatBytes(chunks);
}
function encodeBsatnColumn(value, column) {
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
function encodeBsatnPayload(value, column) {
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
            view.setBigInt64(0, asBigIntInRange(value, column, -(1n << 63n), (1n << 63n) - 1n, "64-bit signed integer"), true);
            return view8;
        case "uint64":
            view.setBigUint64(0, asBigIntInRange(value, column, 0n, (1n << 64n) - 1n, "64-bit unsigned integer"), true);
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
function encodeLengthPrefixedBytes(bytes) {
    const out = new Uint8Array(4 + bytes.length);
    writeUint32LE(out, 0, bytes.length);
    out.set(bytes, 4);
    return out;
}
function encodeStringArray(value, column) {
    if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) {
        throw invalidBsatnValue(column, "string[]");
    }
    const encoded = value.map((item) => utf8Bytes(item, `BSATN ${column.name}`));
    const chunks = [new Uint8Array(4)];
    writeUint32LE(chunks[0], 0, encoded.length);
    for (const item of encoded) {
        chunks.push(encodeLengthPrefixedBytes(item));
    }
    return concatBytes(chunks);
}
function encodeUUID(value, column) {
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
function encodeWideInteger(value, column, byteLength, signed) {
    const bits = BigInt(byteLength * 8);
    const max = signed ? (1n << (bits - 1n)) - 1n : (1n << bits) - 1n;
    const min = signed ? -(1n << (bits - 1n)) : 0n;
    let n = asBigIntInRange(value, column, min, max, signed ? `${byteLength * 8}-bit signed integer` : `${byteLength * 8}-bit unsigned integer`);
    if (n < 0) {
        n = (1n << bits) + n;
    }
    const out = new Uint8Array(byteLength);
    for (let i = 0; i < byteLength; i += 1) {
        out[i] = Number((n >> BigInt(i * 8)) & 0xffn);
    }
    return out;
}
function asInteger(value, column, min, max) {
    if (typeof value !== "number" || !Number.isInteger(value) || value < min || value > max) {
        throw invalidBsatnValue(column, `integer in [${min}, ${max}]`);
    }
    return value;
}
function asNumber(value, column) {
    if (typeof value !== "number" || Number.isNaN(value)) {
        throw invalidBsatnValue(column, "number");
    }
    return value;
}
function asBigInt(value, column) {
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
function asBigIntInRange(value, column, min, max, expected) {
    const n = asBigInt(value, column);
    if (n < min || n > max) {
        throw invalidBsatnValue(column, expected);
    }
    return n;
}
function asString(value, column) {
    if (typeof value !== "string") {
        throw invalidBsatnValue(column, "string");
    }
    return value;
}
function invalidBsatnValue(column, expected) {
    return new ShunterValidationError("Value does not match the BSATN column schema.", {
        code: "bsatn_value_type_mismatch",
        details: { column: column.name, kind: column.kind, expected },
    });
}
function concatBytes(chunks) {
    const length = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
    const out = new Uint8Array(length);
    let offset = 0;
    for (const chunk of chunks) {
        out.set(chunk, offset);
        offset += chunk.length;
    }
    return out;
}
function readBsatnColumn(row, offset, column) {
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
function readBsatnPayload(row, offset, kind, column) {
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
function assertBsatnAvailable(row, offset, length, column) {
    if (row.length < offset + length) {
        throw new ShunterValidationError("Malformed BSATN row: column payload is truncated.", {
            code: "bsatn_truncated_payload",
            details: { column },
        });
    }
}
function readBsatnString(row, offset, column) {
    const [raw, nextOffset] = readBytes(row, offset, `BSATN ${column}`);
    try {
        return [new TextDecoder("utf-8", { fatal: true }).decode(raw), nextOffset];
    }
    catch (error) {
        throw new ShunterValidationError("Malformed BSATN row: string payload is not valid UTF-8.", {
            code: "bsatn_invalid_utf8",
            details: { column },
            cause: error,
        });
    }
}
function readBsatnStringArray(row, offset, column) {
    const [count, countOffset] = readUint32LE(row, offset, `BSATN ${column} array count`);
    offset = countOffset;
    if (count > Math.floor((row.length - offset) / 4)) {
        throw new ShunterValidationError("Malformed BSATN row: array count exceeds remaining bytes.", {
            code: "bsatn_array_count_exceeds_remaining",
            details: { column, count, remainingBytes: row.length - offset },
        });
    }
    const values = [];
    for (let i = 0; i < count; i += 1) {
        const [value, valueOffset] = readBsatnString(row, offset, `${column}[${i}]`);
        values.push(value);
        offset = valueOffset;
    }
    return [values, offset];
}
function readBsatnUUID(row, offset, column) {
    assertBsatnAvailable(row, offset, 16, column);
    const hex = bytesKey(row.slice(offset, offset + 16));
    return [
        `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`,
        offset + 16,
    ];
}
function readBsatnJSON(row, offset, column) {
    const [rawJSON, nextOffset] = readBsatnString(row, offset, column);
    try {
        return [JSON.parse(rawJSON), nextOffset];
    }
    catch (error) {
        throw new ShunterValidationError("Malformed BSATN row: JSON payload is invalid.", {
            code: "bsatn_invalid_json",
            details: { column },
            cause: error,
        });
    }
}
function readBsatnWideInt(row, offset, words, signed, column) {
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
function decodeEnvelopeRowList(rows) {
    return rows.length === 0 ? [] : decodeRowList(rows).rows;
}
function tryDecodeEnvelopeRowList(rows) {
    try {
        return decodeEnvelopeRowList(rows);
    }
    catch {
        return undefined;
    }
}
export function decodeSubscribeSingleAppliedFrame(data) {
    const frame = frameBytes(data);
    if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_SUBSCRIBE_SINGLE_APPLIED) {
        throw new ShunterProtocolError("Expected SubscribeSingleApplied server message.");
    }
    let offset = 1;
    const [requestId, requestOffset] = readUint32LE(frame, offset, "SubscribeSingleApplied request_id");
    offset = requestOffset;
    const [totalHostExecutionDurationMicros, durationOffset] = readUint64LE(frame, offset, "SubscribeSingleApplied total_host_execution_duration_micros");
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
export function decodeUnsubscribeSingleAppliedFrame(data) {
    const frame = frameBytes(data);
    if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_SINGLE_APPLIED) {
        throw new ShunterProtocolError("Expected UnsubscribeSingleApplied server message.");
    }
    let offset = 1;
    const [requestId, requestOffset] = readUint32LE(frame, offset, "UnsubscribeSingleApplied request_id");
    offset = requestOffset;
    const [totalHostExecutionDurationMicros, durationOffset] = readUint64LE(frame, offset, "UnsubscribeSingleApplied total_host_execution_duration_micros");
    offset = durationOffset;
    const [queryId, queryOffset] = readUint32LE(frame, offset, "UnsubscribeSingleApplied query_id");
    offset = queryOffset;
    const [hasRows, rowsTagOffset] = readBooleanTag(frame, offset, "UnsubscribeSingleApplied has_rows");
    offset = rowsTagOffset;
    let rows;
    let rowBytes;
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
export function decodeSubscribeMultiAppliedFrame(data) {
    return decodeSubscriptionSetAppliedFrame(data, SHUNTER_SERVER_MESSAGE_SUBSCRIBE_MULTI_APPLIED, "SubscribeMultiApplied");
}
export function decodeUnsubscribeMultiAppliedFrame(data) {
    return decodeSubscriptionSetAppliedFrame(data, SHUNTER_SERVER_MESSAGE_UNSUBSCRIBE_MULTI_APPLIED, "UnsubscribeMultiApplied");
}
export function decodeSubscriptionErrorFrame(data) {
    const frame = frameBytes(data);
    if (frame.length < 1 || frame[0] !== SHUNTER_SERVER_MESSAGE_SUBSCRIPTION_ERROR) {
        throw new ShunterProtocolError("Expected SubscriptionError server message.");
    }
    let offset = 1;
    const [totalHostExecutionDurationMicros, durationOffset] = readUint64LE(frame, offset, "SubscriptionError total_host_execution_duration_micros");
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
function binaryBytes(data, label) {
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
function frameBytes(data) {
    return binaryBytes(data, "binary WebSocket frame");
}
function readUint32LE(frame, offset, label) {
    if (frame.length < offset + 4) {
        throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
    }
    const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
    return [view.getUint32(offset, true), offset + 4];
}
function readInt64LE(frame, offset, label) {
    if (frame.length < offset + 8) {
        throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
    }
    const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
    return [view.getBigInt64(offset, true), offset + 8];
}
function readUint64LE(frame, offset, label) {
    if (frame.length < offset + 8) {
        throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
    }
    const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
    return [view.getBigUint64(offset, true), offset + 8];
}
function readFixedBytes(frame, offset, length, label) {
    if (frame.length < offset + length) {
        throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
    }
    return [frame.slice(offset, offset + length), offset + length];
}
function readBytes(frame, offset, label) {
    const [length, bytesOffset] = readUint32LE(frame, offset, `${label} length`);
    if (frame.length < bytesOffset + length) {
        throw new ShunterProtocolError(`Malformed frame: ${label} is truncated.`);
    }
    return [frame.slice(bytesOffset, bytesOffset + length), bytesOffset + length];
}
function readStringValue(frame, offset, label) {
    const [raw, nextOffset] = readBytes(frame, offset, label);
    try {
        return [new TextDecoder("utf-8", { fatal: true }).decode(raw), nextOffset];
    }
    catch (error) {
        throw new ShunterProtocolError(`Malformed frame: ${label} is not valid UTF-8.`, { cause: error });
    }
}
function readOptionalStringValue(frame, offset, label) {
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
function readOptionalUint32(frame, offset, label) {
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
function readBooleanTag(frame, offset, label) {
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
function readTransactionUpdateStatus(frame, offset) {
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
function readRawSubscriptionUpdates(frame, offset) {
    const [count, countOffset] = readUint32LE(frame, offset, "SubscriptionUpdate count");
    offset = countOffset;
    if (count > Math.floor((frame.length - offset) / 16)) {
        throw new ShunterProtocolError("Malformed frame: SubscriptionUpdate count exceeds remaining bytes.");
    }
    const updates = [];
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
function readReducerCallInfo(frame, offset) {
    const [name, nameOffset] = readStringValue(frame, offset, "ReducerCallInfo reducer_name");
    offset = nameOffset;
    const [reducerId, reducerOffset] = readUint32LE(frame, offset, "ReducerCallInfo reducer_id");
    offset = reducerOffset;
    const [args, argsOffset] = readBytes(frame, offset, "ReducerCallInfo args");
    offset = argsOffset;
    const [requestId, requestOffset] = readUint32LE(frame, offset, "ReducerCallInfo request_id");
    return [{ name, reducerId, args, requestId }, requestOffset];
}
function readOneOffQueryTables(frame, offset) {
    const [count, countOffset] = readUint32LE(frame, offset, "OneOffQueryResponse table count");
    offset = countOffset;
    if (count > Math.floor((frame.length - offset) / 8)) {
        throw new ShunterProtocolError("Malformed frame: OneOffQueryResponse table count exceeds remaining bytes.");
    }
    const tables = [];
    for (let i = 0; i < count; i += 1) {
        const [tableName, tableOffset] = readStringValue(frame, offset, "OneOffQueryResponse table_name");
        offset = tableOffset;
        const [rows, rowsOffset] = readBytes(frame, offset, "OneOffQueryResponse rows");
        offset = rowsOffset;
        tables.push({ tableName, rows, rowBytes: decodeEnvelopeRowList(rows) });
    }
    return [tables, offset];
}
function decodeSubscriptionSetAppliedFrame(data, expectedTag, label) {
    const frame = frameBytes(data);
    if (frame.length < 1 || frame[0] !== expectedTag) {
        throw new ShunterProtocolError(`Expected ${label} server message.`);
    }
    let offset = 1;
    const [requestId, requestOffset] = readUint32LE(frame, offset, `${label} request_id`);
    offset = requestOffset;
    const [totalHostExecutionDurationMicros, durationOffset] = readUint64LE(frame, offset, `${label} total_host_execution_duration_micros`);
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
function bytesKey(bytes) {
    return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
export function encodeReducerArgs(args, encodeArgs) {
    if (encodeArgs === undefined) {
        if (args instanceof Uint8Array) {
            return new Uint8Array(args);
        }
        throw new ShunterValidationError("Reducer args require an encoder unless they are already Uint8Array.", {
            code: "missing_reducer_arg_encoder",
        });
    }
    const encoded = encodeArgs(args);
    if (!(encoded instanceof Uint8Array)) {
        throw new ShunterValidationError("Reducer arg encoder must return Uint8Array.", {
            code: "invalid_reducer_arg_encoder_result",
        });
    }
    return new Uint8Array(encoded);
}
export function reducerCallOptions(options) {
    return {
        requestId: options.requestId,
        noSuccessNotify: options.noSuccessNotify,
        signal: options.signal,
    };
}
export function reducerCallResultRequestOptions(options) {
    return {
        requestId: options.requestId,
        signal: options.signal,
        decodeResult: options.decodeResult,
    };
}
export async function callReducerWithEncodedArgs(callReducer, name, args, options = {}) {
    const encodedArgs = options.encodeArgs === undefined
        ? encodeReducerArgs(args)
        : encodeReducerArgs(args, options.encodeArgs);
    return callReducer(name, encodedArgs, reducerCallOptions(options));
}
export async function callReducerWithResult(callReducer, name, args, options = {}) {
    let rawResult;
    try {
        rawResult = await callReducer(name, args, {
            requestId: options.requestId,
            signal: options.signal,
        });
    }
    catch (error) {
        const failedUpdate = reducerFailureUpdate(error);
        if (failedUpdate === undefined ||
            failedUpdate.reducerCall.name !== name ||
            (options.requestId !== undefined && failedUpdate.reducerCall.requestId !== options.requestId)) {
            throw error;
        }
        rawResult = failedUpdate.rawFrame;
    }
    return decodeReducerCallResult(name, rawResult, {
        requestId: options.requestId,
        decodeResult: options.decodeResult,
    });
}
function reducerFailureUpdate(error) {
    if (!(error instanceof ShunterValidationError) || error.code !== "reducer_failed") {
        return undefined;
    }
    if (typeof error.details !== "object" || error.details === null) {
        return undefined;
    }
    const details = error.details;
    if (!(details.rawFrame instanceof Uint8Array) ||
        typeof details.reducerCall?.name !== "string" ||
        typeof details.reducerCall.requestId !== "number") {
        return undefined;
    }
    return error.details;
}
export async function callReducerWithEncodedArgsResult(callReducer, name, args, options = {}) {
    const encodedArgs = options.encodeArgs === undefined
        ? encodeReducerArgs(args)
        : encodeReducerArgs(args, options.encodeArgs);
    return callReducerWithResult(callReducer, name, encodedArgs, reducerCallResultRequestOptions(options));
}
export function encodeReducerCallRequest(name, args, options = {}) {
    const requestId = options.requestId ?? 0;
    assertUint32(requestId, "Reducer request ID");
    const flags = reducerCallFlags(options);
    const reducerName = utf8Bytes(name, "Reducer name");
    const frameLength = 1 +
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
export function decodeReducerCallResult(name, data, options = {}) {
    const update = decodeTransactionUpdateFrame(data);
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
            ? rawResult
            : options.decodeResult(update),
        rawResult,
    };
}
function reducerCallFlags(options) {
    return options.noSuccessNotify === true
        ? SHUNTER_CALL_REDUCER_FLAGS_NO_SUCCESS_NOTIFY
        : SHUNTER_CALL_REDUCER_FLAGS_FULL_UPDATE;
}
function assertUint32(value, label) {
    if (!Number.isInteger(value) || value < 0 || value > maxUint32) {
        throw new ShunterValidationError(`${label} must be an unsigned 32-bit integer.`);
    }
}
function utf8Bytes(value, label) {
    if (!isWellFormedUTF16(value)) {
        throw new ShunterValidationError(`${label} must be valid UTF-8.`);
    }
    return new TextEncoder().encode(value);
}
function isWellFormedUTF16(value) {
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
function writeUint32LE(frame, offset, value) {
    new DataView(frame.buffer, frame.byteOffset, frame.byteLength).setUint32(offset, value, true);
    return offset + 4;
}
export function encodeDeclaredQueryRequest(name, options = {}) {
    const requestId = options.requestId;
    if (requestId !== undefined) {
        assertUint32(requestId, "Declared query request ID");
    }
    const messageId = options.messageId === undefined
        ? requestIdMessageId(requestId ?? 0)
        : new Uint8Array(options.messageId);
    const params = optionalUint8Array(options.params, "Declared query parameters");
    const queryName = utf8Bytes(name, "Declared query name");
    const frameLength = 1 +
        4 + messageId.length +
        4 + queryName.length +
        (params === undefined ? 0 : 4 + params.length);
    const frame = new Uint8Array(frameLength);
    let offset = 0;
    frame[offset] = params === undefined
        ? SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY
        : SHUNTER_CLIENT_MESSAGE_DECLARED_QUERY_WITH_PARAMETERS;
    offset += 1;
    offset = writeUint32LE(frame, offset, messageId.length);
    frame.set(messageId, offset);
    offset += messageId.length;
    offset = writeUint32LE(frame, offset, queryName.length);
    frame.set(queryName, offset);
    offset += queryName.length;
    if (params !== undefined) {
        offset = writeUint32LE(frame, offset, params.length);
        frame.set(params, offset);
    }
    return {
        name,
        ...(requestId === undefined ? {} : { requestId }),
        messageId,
        ...(params === undefined ? {} : { params }),
        frame,
    };
}
function requestIdMessageId(requestId) {
    assertUint32(requestId, "Declared query request ID");
    const messageId = new Uint8Array(4);
    writeUint32LE(messageId, 0, requestId);
    return messageId;
}
function optionalUint8Array(value, label) {
    if (value === undefined) {
        return undefined;
    }
    if (!(value instanceof Uint8Array)) {
        throw new ShunterValidationError(`${label} must be a Uint8Array.`, {
            code: "invalid_declared_read_parameters",
            details: { receivedType: value === null ? "null" : typeof value },
        });
    }
    return new Uint8Array(value);
}
function assertDeclaredReadParametersSupported(selectedSubprotocol, expected) {
    if (selectedSubprotocol === SHUNTER_SUBPROTOCOL_V2) {
        return;
    }
    throw new ShunterProtocolMismatchError("The negotiated Shunter WebSocket subprotocol does not support parameterized declared reads.", {
        code: "declared_read_parameters_unsupported_subprotocol",
        expected,
        receivedSubprotocol: selectedSubprotocol,
    });
}
export function createSubscriptionHandle(options = {}) {
    let state = options.initialRows === undefined
        ? { status: "subscribing" }
        : { status: "active", rows: [...options.initialRows] };
    let unsubscribePromise;
    let resolveClosed;
    const closed = new Promise((resolve) => {
        resolveClosed = resolve;
    });
    const setState = (next) => {
        state = next;
        options.onStateChange?.(state);
    };
    const finish = (closedState) => {
        if (state.status === "closed") {
            return;
        }
        setState(closedState.error === undefined
            ? { status: "closed" }
            : { status: "closed", error: closedState.error });
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
        replaceRows(rows) {
            if (state.status === "closed") {
                throw new ShunterClosedClientError("Cannot replace rows on a closed subscription.");
            }
            setState({
                status: state.status === "unsubscribing" ? "unsubscribing" : "active",
                rows: [...rows],
            });
        },
        close(error) {
            finish(error === undefined ? { reason: "closed" } : { reason: "error", error });
        },
        async unsubscribe() {
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
                }
                catch (error) {
                    finish({ reason: "error", error: toShunterError(error, "transport", "Unsubscribe failed") });
                }
            })();
            return unsubscribePromise;
        },
    };
}
export function encodeSubscribeSingleRequest(queryString, options = {}) {
    const requestId = options.requestId ?? 0;
    const queryId = options.queryId ?? 0;
    assertUint32(requestId, "SubscribeSingle request ID");
    assertUint32(queryId, "SubscribeSingle query ID");
    const query = utf8Bytes(queryString, "SubscribeSingle query string");
    const frameLength = 1 +
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
export function encodeTableSubscriptionRequest(table, options = {}) {
    return {
        table,
        ...encodeSubscribeSingleRequest(`SELECT * FROM ${quoteSqlIdentifier(table)}`, options),
    };
}
export function encodeDeclaredViewSubscriptionRequest(name, options = {}) {
    const requestId = options.requestId ?? 0;
    const queryId = options.queryId ?? 0;
    assertUint32(requestId, "Declared view subscription request ID");
    assertUint32(queryId, "Declared view subscription query ID");
    const params = optionalUint8Array(options.params, "Declared view parameters");
    const viewName = utf8Bytes(name, "Declared view name");
    const frameLength = 1 +
        4 +
        4 +
        4 + viewName.length +
        (params === undefined ? 0 : 4 + params.length);
    const frame = new Uint8Array(frameLength);
    let offset = 0;
    frame[offset] = params === undefined
        ? SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW
        : SHUNTER_CLIENT_MESSAGE_SUBSCRIBE_DECLARED_VIEW_WITH_PARAMETERS;
    offset += 1;
    offset = writeUint32LE(frame, offset, requestId);
    offset = writeUint32LE(frame, offset, queryId);
    offset = writeUint32LE(frame, offset, viewName.length);
    frame.set(viewName, offset);
    offset += viewName.length;
    if (params !== undefined) {
        offset = writeUint32LE(frame, offset, params.length);
        frame.set(params, offset);
    }
    return {
        name,
        requestId,
        queryId,
        ...(params === undefined ? {} : { params }),
        frame,
    };
}
export function encodeUnsubscribeSingleRequest(queryId, options = {}) {
    return encodeSubscriptionUnsubscribeRequest(SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_SINGLE, queryId, "UnsubscribeSingle", options);
}
export function encodeUnsubscribeMultiRequest(queryId, options = {}) {
    return encodeSubscriptionUnsubscribeRequest(SHUNTER_CLIENT_MESSAGE_UNSUBSCRIBE_MULTI, queryId, "Unsubscribe", options);
}
function encodeSubscriptionUnsubscribeRequest(tag, queryId, label, options = {}) {
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
function quoteSqlIdentifier(identifier) {
    return `"${identifier.replaceAll('"', '""')}"`;
}
//# sourceMappingURL=index.js.map