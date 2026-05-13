package protocol

import (
	"fmt"
	"unicode/utf8"
)

func validateClientMessageForEncode(m any) error {
	switch msg := m.(type) {
	case SubscribeSingleMsg:
		return requireValidWireString("SubscribeSingle.QueryString", msg.QueryString)
	case CallReducerMsg:
		if err := requireValidWireString("CallReducer.ReducerName", msg.ReducerName); err != nil {
			return err
		}
		if !validCallReducerFlags(msg.Flags) {
			return fmt.Errorf("%w: invalid CallReducer flags byte %d", ErrMalformedMessage, msg.Flags)
		}
	case OneOffQueryMsg:
		return requireValidWireString("OneOffQuery.QueryString", msg.QueryString)
	case DeclaredQueryMsg:
		return requireValidWireString("DeclaredQuery.Name", msg.Name)
	case DeclaredQueryWithParametersMsg:
		return requireValidWireString("DeclaredQueryWithParameters.Name", msg.Name)
	case SubscribeMultiMsg:
		for i, query := range msg.QueryStrings {
			if err := requireValidWireString(fmt.Sprintf("SubscribeMulti.QueryStrings[%d]", i), query); err != nil {
				return err
			}
		}
	case SubscribeDeclaredViewMsg:
		return requireValidWireString("SubscribeDeclaredView.Name", msg.Name)
	case SubscribeDeclaredViewWithParametersMsg:
		return requireValidWireString("SubscribeDeclaredViewWithParameters.Name", msg.Name)
	}
	return nil
}

func validateServerMessageForEncode(m any) error {
	switch msg := m.(type) {
	case IdentityToken:
		return requireValidWireString("IdentityToken.Token", msg.Token)
	case SubscribeSingleApplied:
		return requireValidWireString("SubscribeSingleApplied.TableName", msg.TableName)
	case SubscriptionError:
		return requireValidWireString("SubscriptionError.Error", msg.Error)
	case TransactionUpdate:
		if err := validateUpdateStatusForEncode(msg.Status); err != nil {
			return err
		}
		return validateReducerCallInfoForEncode(msg.ReducerCall)
	case TransactionUpdateLight:
		return validateSubscriptionUpdatesForEncode("TransactionUpdateLight.Update", msg.Update)
	case OneOffQueryResponse:
		if msg.Error != nil {
			if err := requireValidWireString("OneOffQueryResponse.Error", *msg.Error); err != nil {
				return err
			}
		}
		return validateOneOffTablesForEncode(msg.Tables)
	case SubscribeMultiApplied:
		return validateSubscriptionUpdatesForEncode("SubscribeMultiApplied.Update", msg.Update)
	case UnsubscribeMultiApplied:
		return validateSubscriptionUpdatesForEncode("UnsubscribeMultiApplied.Update", msg.Update)
	}
	return nil
}

func validateUpdateStatusForEncode(status UpdateStatus) error {
	switch v := status.(type) {
	case StatusCommitted:
		return validateSubscriptionUpdatesForEncode("StatusCommitted.Update", v.Update)
	case StatusFailed:
		return requireValidWireString("StatusFailed.Error", v.Error)
	case nil:
		return nil
	default:
		return nil
	}
}

func validateReducerCallInfoForEncode(rci ReducerCallInfo) error {
	return requireValidWireString("ReducerCallInfo.ReducerName", rci.ReducerName)
}

func validateSubscriptionUpdatesForEncode(field string, updates []SubscriptionUpdate) error {
	for i, update := range updates {
		if err := requireValidWireString(fmt.Sprintf("%s[%d].TableName", field, i), update.TableName); err != nil {
			return err
		}
	}
	return nil
}

func validateOneOffTablesForEncode(tables []OneOffTable) error {
	for i, table := range tables {
		if err := requireValidWireString(fmt.Sprintf("OneOffQueryResponse.Tables[%d].TableName", i), table.TableName); err != nil {
			return err
		}
	}
	return nil
}

func requireValidWireString(field string, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: invalid UTF-8 string %s", ErrMalformedMessage, field)
	}
	return nil
}

func validCallReducerFlags(flags byte) bool {
	switch flags {
	case CallReducerFlagsFullUpdate, CallReducerFlagsNoSuccessNotify:
		return true
	default:
		return false
	}
}
