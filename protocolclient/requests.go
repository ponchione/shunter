package protocolclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ponchione/shunter/protocol"
)

// ReducerCallRequest describes a reducer execution request.
type ReducerCallRequest struct {
	Name      string
	Arguments []byte
}

// DeclaredQueryRequest describes a declared-query execution request.
type DeclaredQueryRequest struct {
	Name          string
	Parameters    []byte
	HasParameters bool
}

// SQLQueryRequest describes a raw one-off SQL read request.
type SQLQueryRequest struct {
	QueryString string
}

// ProcedureCallRequest describes a procedure execution request.
type ProcedureCallRequest struct {
	Name      string
	Arguments []byte
}

// DialAndCallReducer connects, calls one reducer, and closes the connection.
func DialAndCallReducer(ctx context.Context, opts Options, request ReducerCallRequest) (protocol.IdentityToken, protocol.TransactionUpdate, error) {
	client, identity, err := Dial(ctx, opts)
	if err != nil {
		return protocol.IdentityToken{}, protocol.TransactionUpdate{}, err
	}

	update, callErr := client.CallReducer(ctx, request.Name, request.Arguments)
	runDialAndBeforeCloseHook()
	closeErr := client.Close(ctx)
	if callErr != nil {
		return identity, update, callErr
	}
	if closeErr != nil {
		return identity, update, closeErr
	}
	return identity, update, nil
}

// DialAndExecuteDeclaredQuery connects, executes one declared query, and closes the connection.
func DialAndExecuteDeclaredQuery(ctx context.Context, opts Options, request DeclaredQueryRequest) (protocol.IdentityToken, protocol.OneOffQueryResponse, error) {
	client, identity, err := Dial(ctx, opts)
	if err != nil {
		return protocol.IdentityToken{}, protocol.OneOffQueryResponse{}, err
	}

	response, execErr := client.ExecuteDeclaredQuery(ctx, request)
	runDialAndBeforeCloseHook()
	closeErr := client.Close(ctx)
	if execErr != nil {
		return identity, response, execErr
	}
	if closeErr != nil {
		return identity, response, closeErr
	}
	return identity, response, nil
}

// DialAndExecuteSQLQuery connects, executes one raw SQL read, and closes the connection.
func DialAndExecuteSQLQuery(ctx context.Context, opts Options, request SQLQueryRequest) (protocol.IdentityToken, protocol.OneOffQueryResponse, error) {
	client, identity, err := Dial(ctx, opts)
	if err != nil {
		return protocol.IdentityToken{}, protocol.OneOffQueryResponse{}, err
	}

	response, execErr := client.SQLQuery(ctx, request.QueryString)
	runDialAndBeforeCloseHook()
	closeErr := client.Close(ctx)
	if execErr != nil {
		return identity, response, execErr
	}
	if closeErr != nil {
		return identity, response, closeErr
	}
	return identity, response, nil
}

// DialAndCallProcedure connects, calls one procedure, and closes the connection.
func DialAndCallProcedure(ctx context.Context, opts Options, request ProcedureCallRequest) (protocol.IdentityToken, protocol.ProcedureResponse, error) {
	client, identity, err := Dial(ctx, opts)
	if err != nil {
		return protocol.IdentityToken{}, protocol.ProcedureResponse{}, err
	}

	response, callErr := client.CallProcedure(ctx, request.Name, request.Arguments)
	runDialAndBeforeCloseHook()
	closeErr := client.Close(ctx)
	if callErr != nil {
		return identity, response, callErr
	}
	if closeErr != nil {
		return identity, response, closeErr
	}
	return identity, response, nil
}

// dialAndBeforeCloseHook is test-only instrumentation for contexts that expire
// after a one-off operation succeeds but before connection cleanup begins.
var dialAndBeforeCloseHook func()

func runDialAndBeforeCloseHook() {
	if dialAndBeforeCloseHook != nil {
		dialAndBeforeCloseHook()
	}
}

// CallReducer sends a full-update reducer call and waits for its matching result.
func (c *Client) CallReducer(ctx context.Context, name string, args []byte) (protocol.TransactionUpdate, error) {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	requestID := c.NextRequestID()
	if err := c.Send(ctx, protocol.CallReducerMsg{
		ReducerName: name,
		Args:        args,
		RequestID:   requestID,
		Flags:       protocol.CallReducerFlagsFullUpdate,
	}); err != nil {
		return protocol.TransactionUpdate{}, err
	}

	tag, msg, err := c.readSynchronousResponse(ctx)
	if err != nil {
		return protocol.TransactionUpdate{}, err
	}
	if tag != protocol.TagTransactionUpdate {
		return protocol.TransactionUpdate{}, fmt.Errorf("%w: server tag = %d, want transaction update", ErrUnexpectedMessage, tag)
	}
	update, ok := msg.(protocol.TransactionUpdate)
	if !ok {
		return protocol.TransactionUpdate{}, fmt.Errorf("%w: server message = %T, want protocol.TransactionUpdate", ErrUnexpectedMessage, msg)
	}
	if update.ReducerCall.RequestID != requestID || update.ReducerCall.ReducerName != name {
		return protocol.TransactionUpdate{}, fmt.Errorf(
			"%w: reducer response request=%d name=%q, want request=%d name=%q",
			ErrResponseMismatch,
			update.ReducerCall.RequestID,
			update.ReducerCall.ReducerName,
			requestID,
			name,
		)
	}
	if failed, ok := update.Status.(protocol.StatusFailed); ok {
		return update, fmt.Errorf("%w: %s", ErrReducerFailed, failed.Error)
	}
	return update, nil
}

// DeclaredQuery sends a declared-query request without parameters.
func (c *Client) DeclaredQuery(ctx context.Context, name string) (protocol.OneOffQueryResponse, error) {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	requestID := c.NextRequestID()
	messageID := messageIDFromRequestID(requestID)
	if err := c.Send(ctx, protocol.DeclaredQueryMsg{
		MessageID: messageID,
		Name:      name,
	}); err != nil {
		return protocol.OneOffQueryResponse{}, err
	}
	return c.readDeclaredQueryResponse(ctx, messageID)
}

// SQLQuery sends a raw one-off SQL read request.
func (c *Client) SQLQuery(ctx context.Context, queryString string) (protocol.OneOffQueryResponse, error) {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	requestID := c.NextRequestID()
	messageID := messageIDFromRequestID(requestID)
	if err := c.Send(ctx, protocol.OneOffQueryMsg{
		MessageID:   messageID,
		QueryString: queryString,
	}); err != nil {
		return protocol.OneOffQueryResponse{}, err
	}
	return c.readOneOffQueryResponse(ctx, messageID, ErrSQLQueryFailed, "SQL query")
}

// ExecuteDeclaredQuery sends a declared query, using v2 parameters only when requested.
func (c *Client) ExecuteDeclaredQuery(ctx context.Context, request DeclaredQueryRequest) (protocol.OneOffQueryResponse, error) {
	if request.HasParameters {
		return c.DeclaredQueryWithParameters(ctx, request.Name, request.Parameters)
	}
	return c.DeclaredQuery(ctx, request.Name)
}

// DeclaredQueryWithParameters sends a v2 declared-query request with BSATN parameters.
func (c *Client) DeclaredQueryWithParameters(ctx context.Context, name string, params []byte) (protocol.OneOffQueryResponse, error) {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	if version, ok := protocol.ProtocolVersionForSubprotocol(c.Subprotocol()); !ok || version < protocol.ProtocolVersionV2 {
		return protocol.OneOffQueryResponse{}, fmt.Errorf("%w: negotiated subprotocol %q", ErrProtocolVersion, c.Subprotocol())
	}

	requestID := c.NextRequestID()
	messageID := messageIDFromRequestID(requestID)
	if err := c.Send(ctx, protocol.DeclaredQueryWithParametersMsg{
		MessageID: messageID,
		Name:      name,
		Params:    params,
	}); err != nil {
		return protocol.OneOffQueryResponse{}, err
	}
	return c.readDeclaredQueryResponse(ctx, messageID)
}

func (c *Client) readDeclaredQueryResponse(ctx context.Context, messageID []byte) (protocol.OneOffQueryResponse, error) {
	return c.readOneOffQueryResponse(ctx, messageID, ErrDeclaredQueryFailed, "declared query")
}

func (c *Client) readOneOffQueryResponse(ctx context.Context, messageID []byte, failedErr error, label string) (protocol.OneOffQueryResponse, error) {
	tag, msg, err := c.readSynchronousResponse(ctx)
	if err != nil {
		return protocol.OneOffQueryResponse{}, err
	}
	if tag != protocol.TagOneOffQueryResponse {
		return protocol.OneOffQueryResponse{}, fmt.Errorf("%w: server tag = %d, want one-off query response", ErrUnexpectedMessage, tag)
	}
	response, ok := msg.(protocol.OneOffQueryResponse)
	if !ok {
		return protocol.OneOffQueryResponse{}, fmt.Errorf("%w: server message = %T, want protocol.OneOffQueryResponse", ErrUnexpectedMessage, msg)
	}
	if !bytes.Equal(response.MessageID, messageID) {
		return protocol.OneOffQueryResponse{}, fmt.Errorf("%w: %s message ID %x, want %x", ErrResponseMismatch, label, response.MessageID, messageID)
	}
	if response.Error != nil {
		return response, fmt.Errorf("%w: %s", failedErr, *response.Error)
	}
	return response, nil
}

// CallProcedure sends a procedure request and waits for its matching result.
func (c *Client) CallProcedure(ctx context.Context, name string, args []byte) (protocol.ProcedureResponse, error) {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	if version, ok := protocol.ProtocolVersionForSubprotocol(c.Subprotocol()); !ok || version < protocol.ProtocolVersionV2 {
		return protocol.ProcedureResponse{}, fmt.Errorf("%w: negotiated subprotocol %q", ErrProtocolVersion, c.Subprotocol())
	}
	requestID := c.NextRequestID()
	messageID := messageIDFromRequestID(requestID)
	if err := c.Send(ctx, protocol.CallProcedureMsg{
		MessageID: messageID,
		Name:      name,
		Args:      args,
	}); err != nil {
		return protocol.ProcedureResponse{}, err
	}
	tag, msg, err := c.readSynchronousResponse(ctx)
	if err != nil {
		return protocol.ProcedureResponse{}, err
	}
	if tag != protocol.TagProcedureResponse {
		return protocol.ProcedureResponse{}, fmt.Errorf("%w: server tag = %d, want procedure response", ErrUnexpectedMessage, tag)
	}
	response, ok := msg.(protocol.ProcedureResponse)
	if !ok {
		return protocol.ProcedureResponse{}, fmt.Errorf("%w: server message = %T, want protocol.ProcedureResponse", ErrUnexpectedMessage, msg)
	}
	if !bytes.Equal(response.MessageID, messageID) {
		return protocol.ProcedureResponse{}, fmt.Errorf("%w: procedure message ID %x, want %x", ErrResponseMismatch, response.MessageID, messageID)
	}
	if response.Error != nil {
		return response, fmt.Errorf("%w: %s", ErrProcedureFailed, *response.Error)
	}
	return response, nil
}

func messageIDFromRequestID(requestID uint32) []byte {
	var messageID [4]byte
	binary.LittleEndian.PutUint32(messageID[:], requestID)
	return messageID[:]
}
