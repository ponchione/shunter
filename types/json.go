package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// ErrInvalidJSON identifies payloads that are not valid Shunter canonical JSON.
var ErrInvalidJSON = errors.New("invalid JSON")

// NewJSON builds a JSON value from raw JSON bytes.
// The stored payload is canonicalized by removing insignificant whitespace and
// sorting object keys. Duplicate object keys are rejected.
func NewJSON(x []byte) (Value, error) {
	canonical, err := canonicalizeJSON(x)
	if err != nil {
		return Value{}, err
	}
	return Value{kind: KindJSON, buf: canonical}, nil
}

func canonicalizeJSON(x []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(x))
	dec.UseNumber()
	var out bytes.Buffer
	if err := appendCanonicalJSONValue(&out, dec); err != nil {
		return nil, err
	}
	if tok, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("shunter: %w: trailing token %v", ErrInvalidJSON, tok)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("shunter: %w: %v", ErrInvalidJSON, err)
	}
	return out.Bytes(), nil
}

func appendCanonicalJSONValue(out *bytes.Buffer, dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("shunter: %w: %v", ErrInvalidJSON, err)
	}
	switch tok := tok.(type) {
	case json.Delim:
		switch tok {
		case '{':
			return appendCanonicalJSONObject(out, dec)
		case '[':
			return appendCanonicalJSONArray(out, dec)
		default:
			return fmt.Errorf("shunter: %w: unexpected delimiter %q", ErrInvalidJSON, tok)
		}
	case string:
		return appendCanonicalJSONString(out, tok)
	case json.Number:
		out.WriteString(tok.String())
		return nil
	case bool:
		if tok {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
		return nil
	case nil:
		out.WriteString("null")
		return nil
	default:
		return fmt.Errorf("shunter: %w: unexpected token %T", ErrInvalidJSON, tok)
	}
}

type jsonMember struct {
	key   string
	value []byte
}

func appendCanonicalJSONObject(out *bytes.Buffer, dec *json.Decoder) error {
	seen := map[string]struct{}{}
	var members []jsonMember
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("shunter: %w: %v", ErrInvalidJSON, err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("shunter: %w: object key token %T", ErrInvalidJSON, keyTok)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("shunter: %w: duplicate object key %q", ErrInvalidJSON, key)
		}
		seen[key] = struct{}{}
		var value bytes.Buffer
		if err := appendCanonicalJSONValue(&value, dec); err != nil {
			return err
		}
		members = append(members, jsonMember{key: key, value: append([]byte(nil), value.Bytes()...)})
	}
	end, err := dec.Token()
	if err != nil {
		return fmt.Errorf("shunter: %w: %v", ErrInvalidJSON, err)
	}
	if delim, ok := end.(json.Delim); !ok || delim != '}' {
		return fmt.Errorf("shunter: %w: expected object end", ErrInvalidJSON)
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].key < members[j].key
	})
	out.WriteByte('{')
	for i, member := range members {
		if i > 0 {
			out.WriteByte(',')
		}
		if err := appendCanonicalJSONString(out, member.key); err != nil {
			return err
		}
		out.WriteByte(':')
		out.Write(member.value)
	}
	out.WriteByte('}')
	return nil
}

func appendCanonicalJSONArray(out *bytes.Buffer, dec *json.Decoder) error {
	out.WriteByte('[')
	first := true
	for dec.More() {
		if !first {
			out.WriteByte(',')
		}
		first = false
		if err := appendCanonicalJSONValue(out, dec); err != nil {
			return err
		}
	}
	end, err := dec.Token()
	if err != nil {
		return fmt.Errorf("shunter: %w: %v", ErrInvalidJSON, err)
	}
	if delim, ok := end.(json.Delim); !ok || delim != ']' {
		return fmt.Errorf("shunter: %w: expected array end", ErrInvalidJSON)
	}
	out.WriteByte(']')
	return nil
}

func appendCanonicalJSONString(out *bytes.Buffer, value string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("shunter: %w: %v", ErrInvalidJSON, err)
	}
	out.Write(encoded)
	return nil
}
