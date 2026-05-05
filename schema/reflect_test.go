package schema

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type AllTypes struct {
	A bool `shunter:"primarykey"`
	B int8
	C uint8
	D int16
	E uint16
	F int32
	G uint32
	H int64
	I uint64
	J float32
	K float64
	L string
	M []byte
}

func TestDiscoverFieldsAllTypes(t *testing.T) {
	fields, err := discoverFields(reflect.TypeFor[AllTypes](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 13 {
		t.Fatalf("expected 13 fields, got %d", len(fields))
	}
}

func TestDiscoverFieldsTimestamp(t *testing.T) {
	type WithTimestamp struct {
		ID        uint64 `shunter:"primarykey"`
		CreatedAt time.Time
		TTL       time.Duration
	}
	fields, err := discoverFields(reflect.TypeFor[WithTimestamp](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fields))
	}
	if fields[1].ColumnName != "created_at" || fields[1].Type != KindTimestamp {
		t.Fatalf("timestamp field = %+v, want created_at KindTimestamp", fields[1])
	}
	if fields[2].ColumnName != "ttl" || fields[2].Type != KindDuration {
		t.Fatalf("duration field = %+v, want ttl KindDuration", fields[2])
	}
}

func TestDiscoverFieldsJSON(t *testing.T) {
	type WithJSON struct {
		ID       uint64 `shunter:"primarykey"`
		Metadata json.RawMessage
	}
	fields, err := discoverFields(reflect.TypeFor[WithJSON](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[1].ColumnName != "metadata" || fields[1].Type != KindJSON {
		t.Fatalf("JSON field = %+v, want metadata KindJSON", fields[1])
	}
}

type BadPtr struct {
	X *int64
}

func TestDiscoverFieldsUnsupportedType(t *testing.T) {
	_, err := discoverFields(reflect.TypeFor[BadPtr](), "")
	if err == nil {
		t.Fatal("expected error for *int64 field")
	}
}

type PlatformInt struct {
	X int
}

func TestDiscoverFieldsPlatformInt(t *testing.T) {
	_, err := discoverFields(reflect.TypeFor[PlatformInt](), "")
	if err == nil {
		t.Fatal("expected error for int field")
	}
}

func TestDiscoverFieldsRejectsUniqueWithPlainIndex(t *testing.T) {
	type T struct {
		Email string `shunter:"unique,index"`
	}

	_, err := discoverFields(reflect.TypeFor[T](), "")
	if err == nil {
		t.Fatal("expected unique + plain index tag to be rejected")
	}
}

type WithUnexported struct {
	Exported string
	_        string
}

func TestDiscoverFieldsSkipsUnexported(t *testing.T) {
	fields, err := discoverFields(reflect.TypeFor[WithUnexported](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected 1 field (unexported skipped), got %d", len(fields))
	}
}

type WithExclude struct {
	A string
	B string `shunter:"-"`
}

func TestDiscoverFieldsExclude(t *testing.T) {
	fields, err := discoverFields(reflect.TypeFor[WithExclude](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected 1 field (excluded skipped), got %d", len(fields))
	}
}

type Base struct {
	ID uint64 `shunter:"primarykey"`
}

type WithEmbed struct {
	Base
	Name string
}

func TestDiscoverFieldsEmbedded(t *testing.T) {
	fields, err := discoverFields(reflect.TypeFor[WithEmbed](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields (embedded flattened), got %d", len(fields))
	}
	if fields[0].FieldName != "ID" {
		t.Fatalf("first field should be ID from embedded struct")
	}
}

type PtrEmbed struct {
	*Base
	Name string
}

func TestDiscoverFieldsEmbeddedPtrErrors(t *testing.T) {
	_, err := discoverFields(reflect.TypeFor[PtrEmbed](), "")
	if err == nil {
		t.Fatal("expected error for embedded pointer-to-struct")
	}
}

type ExcludedEmbedded struct {
	Base `shunter:"-"`
	Name string
}

type ExcludedEmbeddedPtr struct {
	*Base `shunter:"-"`
	Name  string
}

func TestDiscoverFieldsExcludedEmbeddedStructSkipsAnonymousField(t *testing.T) {
	fields, err := discoverFields(reflect.TypeFor[ExcludedEmbedded](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected only outer field after excluded embed, got %d", len(fields))
	}
	if fields[0].FieldName != "Name" {
		t.Fatalf("expected remaining field Name, got %q", fields[0].FieldName)
	}
}

func TestDiscoverFieldsExcludedEmbeddedPointerSkipsAnonymousField(t *testing.T) {
	fields, err := discoverFields(reflect.TypeFor[ExcludedEmbeddedPtr](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected only outer field after excluded embedded pointer, got %d", len(fields))
	}
	if fields[0].FieldName != "Name" {
		t.Fatalf("expected remaining field Name, got %q", fields[0].FieldName)
	}
}

type DeepBase struct {
	Base
	Extra int32
}
type DeepEmbed struct {
	DeepBase
	Score int64
}

func TestDiscoverFieldsDeeplyNested(t *testing.T) {
	fields, err := discoverFields(reflect.TypeFor[DeepEmbed](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// DeepBase.Base.ID, DeepBase.Extra, Score
	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fields))
	}
}

func TestDiscoverFieldsNameOverride(t *testing.T) {
	type T struct {
		MyField string `shunter:"name:custom_name"`
	}
	fields, err := discoverFields(reflect.TypeFor[T](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fields[0].ColumnName != "custom_name" {
		t.Fatalf("expected column name 'custom_name', got %q", fields[0].ColumnName)
	}
}

func TestDiscoverFieldsSnakeCaseDefault(t *testing.T) {
	type T struct {
		PlayerID uint64
	}
	fields, err := discoverFields(reflect.TypeFor[T](), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fields[0].ColumnName != "player_id" {
		t.Fatalf("expected 'player_id', got %q", fields[0].ColumnName)
	}
}
