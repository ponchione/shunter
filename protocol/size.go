package protocol

import (
	"fmt"

	"github.com/ponchione/shunter/schema"
)

const (
	sizeByte          = 1
	sizeUint32        = 4
	sizeUint64        = 8
	sizeInt64         = 8
	sizeIdentity      = 32
	sizeConnectionID  = 16
	sizeAppliedHeader = sizeUint32 + sizeUint64 + sizeUint32
)

type protocolSizer struct {
	n   int
	err error
}

func (s *protocolSizer) add(n int) {
	if s.err != nil {
		return
	}
	if n < 0 || s.n > int(^uint(0)>>1)-n {
		s.err = fmt.Errorf("%w: encoded message size overflows int", ErrMessageTooLarge)
		return
	}
	s.n += n
}

func (s *protocolSizer) string(v string) {
	if _, err := checkedProtocolLen("string", len(v)); err != nil {
		s.err = err
		return
	}
	s.add(sizeUint32 + len(v))
}

func (s *protocolSizer) bytes(v []byte) {
	if _, err := checkedProtocolLen("bytes", len(v)); err != nil {
		s.err = err
		return
	}
	s.add(sizeUint32 + len(v))
}

func (s *protocolSizer) count(label string, n int) {
	if _, err := checkedProtocolLen(label, n); err != nil {
		s.err = err
		return
	}
	s.add(sizeUint32)
}

func (s *protocolSizer) optionalString(v *string) {
	s.add(sizeByte)
	if v != nil {
		s.string(*v)
	}
}

func (s *protocolSizer) optionalUint32(v *uint32) {
	s.add(sizeByte)
	if v != nil {
		s.add(sizeUint32)
	}
}

func (s *protocolSizer) optionalTableID(v *schema.TableID) {
	s.add(sizeByte)
	if v != nil {
		s.add(sizeUint32)
	}
}

func (s *protocolSizer) size() (int, error) {
	return s.n, s.err
}

func encodedClientMessageSize(m any) (int, error) {
	var s protocolSizer
	s.add(sizeByte)
	switch msg := m.(type) {
	case SubscribeSingleMsg:
		s.add(sizeUint32 + sizeUint32)
		s.string(msg.QueryString)
	case UnsubscribeSingleMsg:
		s.add(sizeUint32 + sizeUint32)
	case CallReducerMsg:
		s.string(msg.ReducerName)
		s.bytes(msg.Args)
		s.add(sizeUint32 + sizeByte)
	case OneOffQueryMsg:
		s.bytes(msg.MessageID)
		s.string(msg.QueryString)
	case DeclaredQueryMsg:
		s.bytes(msg.MessageID)
		s.string(msg.Name)
	case SubscribeMultiMsg:
		s.add(sizeUint32 + sizeUint32)
		s.count("SubscribeMulti query string count", len(msg.QueryStrings))
		for _, qs := range msg.QueryStrings {
			s.string(qs)
		}
	case SubscribeDeclaredViewMsg:
		s.add(sizeUint32 + sizeUint32)
		s.string(msg.Name)
	case UnsubscribeMultiMsg:
		s.add(sizeUint32 + sizeUint32)
	default:
		return 0, fmt.Errorf("%w: %T", ErrUnknownMessageTag, m)
	}
	return s.size()
}

func encodedServerMessageSize(m any) (int, error) {
	var s protocolSizer
	s.add(sizeByte)
	switch msg := m.(type) {
	case IdentityToken:
		s.add(sizeIdentity)
		s.string(msg.Token)
		s.add(sizeConnectionID)
	case SubscribeSingleApplied:
		s.add(sizeAppliedHeader)
		s.string(msg.TableName)
		s.bytes(msg.Rows)
	case UnsubscribeSingleApplied:
		s.add(sizeAppliedHeader + sizeByte)
		if msg.HasRows {
			s.bytes(msg.Rows)
		}
	case SubscriptionError:
		s.add(sizeUint64)
		s.optionalUint32(msg.RequestID)
		s.optionalUint32(msg.QueryID)
		s.optionalTableID(msg.TableID)
		s.string(msg.Error)
	case TransactionUpdate:
		s.updateStatus(msg.Status)
		s.add(sizeInt64 + sizeIdentity + sizeConnectionID)
		s.reducerCallInfo(msg.ReducerCall)
		s.add(sizeInt64)
	case TransactionUpdateLight:
		s.add(sizeUint32)
		s.subscriptionUpdates(msg.Update)
	case OneOffQueryResponse:
		s.bytes(msg.MessageID)
		s.optionalString(msg.Error)
		s.oneOffTables(msg.Tables)
		s.add(sizeInt64)
	case SubscribeMultiApplied:
		s.add(sizeAppliedHeader)
		s.subscriptionUpdates(msg.Update)
	case UnsubscribeMultiApplied:
		s.add(sizeAppliedHeader)
		s.subscriptionUpdates(msg.Update)
	default:
		return 0, fmt.Errorf("%w: %T", ErrUnknownMessageTag, m)
	}
	return s.size()
}

func (s *protocolSizer) updateStatus(status UpdateStatus) {
	s.add(sizeByte)
	switch v := status.(type) {
	case StatusCommitted:
		s.subscriptionUpdates(v.Update)
	case StatusFailed:
		s.string(v.Error)
	}
}

func (s *protocolSizer) reducerCallInfo(rci ReducerCallInfo) {
	s.string(rci.ReducerName)
	s.add(sizeUint32)
	s.bytes(rci.Args)
	s.add(sizeUint32)
}

func (s *protocolSizer) subscriptionUpdates(ups []SubscriptionUpdate) {
	s.count("SubscriptionUpdate count", len(ups))
	for _, u := range ups {
		s.add(sizeUint32)
		s.string(u.TableName)
		s.bytes(u.Inserts)
		s.bytes(u.Deletes)
	}
}

func (s *protocolSizer) oneOffTables(tables []OneOffTable) {
	s.count("OneOffTable count", len(tables))
	for _, t := range tables {
		s.string(t.TableName)
		s.bytes(t.Rows)
	}
}
