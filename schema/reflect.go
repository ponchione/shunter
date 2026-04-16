package schema

import (
	"fmt"
	"reflect"
)

// fieldInfo holds intermediate per-field metadata from struct reflection.
type fieldInfo struct {
	FieldName  string
	ColumnName string
	Type       ValueKind
	Tags       *TagDirectives
}

// discoverFields walks a struct's exported fields via reflect, resolves types,
// parses tags, handles embedding, and returns ordered field metadata.
func discoverFields(t reflect.Type, prefix string) ([]fieldInfo, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("schema error: %s: expected struct, got %s", t.Name(), t.Kind())
	}
	if prefix == "" {
		prefix = t.Name()
	}

	var fields []fieldInfo

	for i := range t.NumField() {
		f := t.Field(i)

		// Skip unexported fields (unless embedded).
		if !f.IsExported() {
			continue
		}

		path := prefix + "." + f.Name

		// Parse tag before any embedding behavior so shunter:"-" exclusion
		// applies uniformly to ordinary and anonymous fields.
		raw := f.Tag.Get("shunter")
		td, err := ParseTag(raw)
		if err != nil {
			return nil, fmt.Errorf("schema error: %s: %w", path, err)
		}

		// Skip excluded fields.
		if td.Exclude {
			continue
		}

		// Handle embedded structs.
		if f.Anonymous {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				return nil, fmt.Errorf("schema error: %s: embedded pointer-to-struct is not supported", path)
			}
			if ft.Kind() == reflect.Struct {
				sub, err := discoverFields(ft, path)
				if err != nil {
					return nil, err
				}
				fields = append(fields, sub...)
				continue
			}
			// Non-struct anonymous field — fall through to normal processing.
		}

		// Map Go type to ValueKind.
		vk, err := GoTypeToValueKind(f.Type)
		if err != nil {
			return nil, fmt.Errorf("schema error: %s: %w", path, err)
		}

		// Derive column name.
		colName := td.NameOverride
		if colName == "" {
			colName = ToSnakeCase(f.Name)
		}

		fields = append(fields, fieldInfo{
			FieldName:  f.Name,
			ColumnName: colName,
			Type:       vk,
			Tags:       td,
		})
	}

	return fields, nil
}
