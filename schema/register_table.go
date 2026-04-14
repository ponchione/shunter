package schema

import (
	"fmt"
	"reflect"
)

// RegisterTable registers a Go struct type as a table with the builder.
// T must be a non-pointer struct type.
func RegisterTable[T any](b *Builder, opts ...TableOption) error {
	t := reflect.TypeFor[T]()

	if t.Kind() == reflect.Pointer {
		return fmt.Errorf("schema error: RegisterTable requires a struct type, got pointer %s", t)
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("schema error: RegisterTable requires a struct type, got %s", t)
	}

	fields, err := discoverFields(t, "")
	if err != nil {
		return err
	}

	def, err := buildTableDefinition(t.Name(), fields, opts...)
	if err != nil {
		return err
	}

	b.TableDef(def, opts...)
	return nil
}
