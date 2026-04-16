package schema

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SnapshotSchema is the serializable schema shape persisted in snapshots for
// startup compatibility checks.
type SnapshotSchema struct {
	Version uint32
	Tables  []TableSchema
}

// ErrSchemaMismatch classifies startup schema incompatibility.
var ErrSchemaMismatch = errors.New("schema mismatch")

// SchemaMismatchError carries human-readable mismatch detail.
type SchemaMismatchError struct {
	Detail string
}

func (e *SchemaMismatchError) Error() string {
	if e == nil || e.Detail == "" {
		return ErrSchemaMismatch.Error()
	}
	return fmt.Sprintf("%s: %s", ErrSchemaMismatch, e.Detail)
}

func (e *SchemaMismatchError) Unwrap() error { return ErrSchemaMismatch }

// CheckSchemaCompatibility compares the registered schema against the schema
// stored in the latest snapshot. Nil snapshot means fresh start and is always
// compatible.
func CheckSchemaCompatibility(registered SchemaRegistry, snapshot *SnapshotSchema) error {
	if snapshot == nil {
		return nil
	}
	if registered == nil {
		return &SchemaMismatchError{Detail: "registered schema is nil"}
	}

	var diffs []string
	if registered.Version() != snapshot.Version {
		diffs = append(diffs, fmt.Sprintf("version mismatch: registered=%d snapshot=%d", registered.Version(), snapshot.Version))
	}

	registeredIDs := registered.Tables()
	if len(registeredIDs) != len(snapshot.Tables) {
		diffs = append(diffs, fmt.Sprintf("table count mismatch: registered=%d snapshot=%d", len(registeredIDs), len(snapshot.Tables)))
	}

	snapshotByID := make(map[TableID]TableSchema, len(snapshot.Tables))
	for _, table := range snapshot.Tables {
		snapshotByID[table.ID] = table
	}
	for _, table := range snapshot.Tables {
		found := false
		for _, id := range registeredIDs {
			if id == table.ID {
				found = true
				break
			}
		}
		if !found {
			diffs = append(diffs, fmt.Sprintf("snapshot has unexpected table id %d (%s)", table.ID, table.Name))
		}
	}

	for _, id := range registeredIDs {
		registeredTable, ok := registered.Table(id)
		if !ok {
			diffs = append(diffs, fmt.Sprintf("registered schema missing table id %d", id))
			continue
		}
		snapshotTable, ok := snapshotByID[id]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("snapshot missing table id %d (%s)", id, registeredTable.Name))
			continue
		}
		compareTableSchema(*registeredTable, snapshotTable, &diffs)
	}

	if len(diffs) == 0 {
		return nil
	}
	return &SchemaMismatchError{Detail: strings.Join(diffs, "; ")}
}

func compareTableSchema(registered, snapshot TableSchema, diffs *[]string) {
	if registered.Name != snapshot.Name {
		*diffs = append(*diffs, fmt.Sprintf("table %d name mismatch: registered=%q snapshot=%q", registered.ID, registered.Name, snapshot.Name))
	}
	if len(registered.Columns) != len(snapshot.Columns) {
		*diffs = append(*diffs, fmt.Sprintf("table %q column count mismatch: registered=%d snapshot=%d", registered.Name, len(registered.Columns), len(snapshot.Columns)))
	} else {
		for i := range registered.Columns {
			rc := registered.Columns[i]
			sc := snapshot.Columns[i]
			if rc.Index != sc.Index || rc.Name != sc.Name || rc.Type != sc.Type || rc.Nullable != sc.Nullable || rc.AutoIncrement != sc.AutoIncrement {
				*diffs = append(*diffs, fmt.Sprintf("table %q column %d mismatch: registered={index:%d name:%q type:%s nullable:%t autoincrement:%t} snapshot={index:%d name:%q type:%s nullable:%t autoincrement:%t}", registered.Name, i, rc.Index, rc.Name, rc.Type, rc.Nullable, rc.AutoIncrement, sc.Index, sc.Name, sc.Type, sc.Nullable, sc.AutoIncrement))
			}
		}
	}
	if len(registered.Indexes) != len(snapshot.Indexes) {
		*diffs = append(*diffs, fmt.Sprintf("table %q index count mismatch: registered=%d snapshot=%d", registered.Name, len(registered.Indexes), len(snapshot.Indexes)))
	} else {
		for i := range registered.Indexes {
			ri := registered.Indexes[i]
			si := snapshot.Indexes[i]
			if ri.Name != si.Name || ri.Unique != si.Unique || ri.Primary != si.Primary || !equalIntSlices(ri.Columns, si.Columns) {
				*diffs = append(*diffs, fmt.Sprintf("table %q index %d mismatch: registered={name:%q columns:%v unique:%t primary:%t} snapshot={name:%q columns:%v unique:%t primary:%t}", registered.Name, i, ri.Name, ri.Columns, ri.Unique, ri.Primary, si.Name, si.Columns, si.Unique, si.Primary))
			}
		}
	}
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Start performs runtime initialization, including schema compatibility checks
// against snapshot metadata when startup recovery has supplied it.
func (e *Engine) Start(_ context.Context) error {
	return CheckSchemaCompatibility(e.registry, e.opts.StartupSnapshotSchema)
}
