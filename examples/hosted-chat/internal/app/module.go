package app

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const messagesTableID schema.TableID = 0

var sendMessageArgsSchema = schema.TableSchema{
	Columns: []schema.ColumnSchema{
		{Index: 0, Name: "author", Type: types.KindString},
		{Index: 1, Name: "body", Type: types.KindString},
	},
}

// Module declares the hosted-chat Shunter module used by the example server
// and contract export binary.
func Module() *shunter.Module {
	return shunter.NewModule("hosted_chat").
		Version("v0.1.0").
		SchemaVersion(1).
		TableDef(schema.TableDefinition{
			Name: "messages",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
				{Name: "author", Type: types.KindString},
				{Name: "body", Type: types.KindString},
			},
		}).
		Reducer("send_message", sendMessage, shunter.WithReducerArgs(shunter.ProductSchema{
			Columns: []shunter.ProductColumn{
				{Name: "author", Type: "string"},
				{Name: "body", Type: "string"},
			},
		})).
		Query(shunter.QueryDeclaration{
			Name: "recent_messages",
			SQL:  "SELECT * FROM messages ORDER BY id DESC LIMIT 20",
		}).
		View(shunter.ViewDeclaration{
			Name: "live_messages",
			SQL:  "SELECT * FROM messages",
		})
}

func sendMessage(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	values, err := bsatn.DecodeProductValue(bytes.NewReader(args), &sendMessageArgsSchema)
	if err != nil {
		return nil, fmt.Errorf("decode send_message args: %w", err)
	}
	author := strings.TrimSpace(values[0].AsString())
	body := strings.TrimSpace(values[1].AsString())
	if author == "" {
		return nil, fmt.Errorf("author is required")
	}
	if body == "" {
		return nil, fmt.Errorf("body is required")
	}

	_, err = ctx.DB.Insert(uint32(messagesTableID), types.ProductValue{
		types.NewUint64(0),
		types.NewString(author),
		types.NewString(body),
	})
	return nil, err
}

// EncodeSendMessageArgs is used by Go tests and small local tools. Browser
// clients normally use the generated TypeScript encoder.
func EncodeSendMessageArgs(author, body string) ([]byte, error) {
	return bsatn.AppendProductValueForSchema(nil, types.ProductValue{
		types.NewString(author),
		types.NewString(body),
	}, &sendMessageArgsSchema)
}
