package protocolclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ponchione/shunter/protocol"
)

// CallReducer sends a full-update reducer call and waits for its matching result.
func (c *Client) CallReducer(ctx context.Context, name string, args []byte) (protocol.TransactionUpdate, error) {
	requestID := c.NextRequestID()
	if err := c.Send(ctx, protocol.CallReducerMsg{
		ReducerName: name,
		Args:        args,
		RequestID:   requestID,
		Flags:       protocol.CallReducerFlagsFullUpdate,
	}); err != nil {
		return protocol.TransactionUpdate{}, err
	}

	tag, msg, err := c.Read(ctx)
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

// DeclaredQueryWithParameters sends a v2 declared-query request with BSATN parameters.
func (c *Client) DeclaredQueryWithParameters(ctx context.Context, name string, params []byte) (protocol.OneOffQueryResponse, error) {
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
	tag, msg, err := c.Read(ctx)
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
		return protocol.OneOffQueryResponse{}, fmt.Errorf("%w: declared query message ID %x, want %x", ErrResponseMismatch, response.MessageID, messageID)
	}
	if response.Error != nil {
		return response, fmt.Errorf("%w: %s", ErrDeclaredQueryFailed, *response.Error)
	}
	return response, nil
}

func messageIDFromRequestID(requestID uint32) []byte {
	var messageID [4]byte
	binary.LittleEndian.PutUint32(messageID[:], requestID)
	return messageID[:]
}
