package protocol

import "errors"

// ErrUnknownMessageTag is returned when a decoded tag byte does not
// match any known client or server message type (SPEC-005 §6).
var ErrUnknownMessageTag = errors.New("protocol: unknown message tag")

// ErrMalformedMessage is returned when a wire body cannot be decoded —
// truncation, length-prefix/payload mismatch, or schema violation
// during decode.
var ErrMalformedMessage = errors.New("protocol: malformed message body")

// ErrMessageTooLarge is returned when a decoded wire message exceeds the
// configured transport size limit.
var ErrMessageTooLarge = errors.New("protocol: message too large")

// ErrOutboundMessageLimit reports a server message rejected before frame
// allocation because it exceeds the configured outbound byte cap.
var ErrOutboundMessageLimit = errors.New("protocol: outbound message limit exceeded")

// ErrExecutorAdmissionRejected marks a connection rejected by executor
// admission during OnConnect.
var ErrExecutorAdmissionRejected = errors.New("protocol: executor admission rejected")

// ErrConnectionIDInUse marks a client-supplied ConnectionID that is already
// owned by a live or admitting connection.
var ErrConnectionIDInUse = errors.New("protocol: connection_id already in use")

// ErrConnectionManagerClosed marks admission rejected after runtime shutdown
// established its connection-manager barrier.
var ErrConnectionManagerClosed = errors.New("protocol: connection manager closed")

//lint:ignore ST1005 Pinned SQL contract tests assert this user-visible diagnostic.
var errSubscriptionRequiresTableShape = errors.New("Column projections are not supported in subscriptions; Subscriptions must return a table type")
