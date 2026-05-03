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

// ErrExecutorAdmissionRejected marks a connection rejected by executor
// admission during OnConnect.
var ErrExecutorAdmissionRejected = errors.New("protocol: executor admission rejected")

// ErrConnectionIDInUse marks a client-supplied ConnectionID that is already
// owned by a live or admitting connection.
var ErrConnectionIDInUse = errors.New("protocol: connection_id already in use")
