package schema

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

type SchemaCompatibilityStatus string

const (
	SchemaCompatibilityCompatible SchemaCompatibilityStatus = "compatible"
	SchemaCompatibilityAdditive   SchemaCompatibilityStatus = "additive"
	SchemaCompatibilityBlocked    SchemaCompatibilityStatus = "blocked"
)

type SchemaCompatibilityChangeKind string

const (
	SchemaCompatibilityChangeSchemaVersion SchemaCompatibilityChangeKind = "schema_version"
	SchemaCompatibilityChangeAddTable      SchemaCompatibilityChangeKind = "add_table"
	SchemaCompatibilityChangeAddIndex      SchemaCompatibilityChangeKind = "add_index"
)

type SchemaCompatibilityIssueKind string

const (
	SchemaCompatibilityIssueMissingRegistry SchemaCompatibilityIssueKind = "missing_registry"
	SchemaCompatibilityIssueDropTable       SchemaCompatibilityIssueKind = "drop_table"
	SchemaCompatibilityIssueTableMismatch   SchemaCompatibilityIssueKind = "table_mismatch"
	SchemaCompatibilityIssueColumnMismatch  SchemaCompatibilityIssueKind = "column_mismatch"
	SchemaCompatibilityIssueIndexMismatch   SchemaCompatibilityIssueKind = "index_mismatch"
)

type SchemaCompatibilityChange struct {
	Kind   SchemaCompatibilityChangeKind `json:"kind"`
	Table  string                        `json:"table,omitempty"`
	Index  string                        `json:"index,omitempty"`
	Detail string                        `json:"detail"`
}

type SchemaCompatibilityIssue struct {
	Kind   SchemaCompatibilityIssueKind `json:"kind"`
	Table  string                       `json:"table,omitempty"`
	Index  string                       `json:"index,omitempty"`
	Detail string                       `json:"detail"`
}

type SchemaCompatibilityReport struct {
	Compatible        bool                        `json:"compatible"`
	Status            SchemaCompatibilityStatus   `json:"status"`
	RegisteredVersion uint32                      `json:"registered_version"`
	SnapshotVersion   uint32                      `json:"snapshot_version"`
	Changes           []SchemaCompatibilityChange `json:"changes,omitempty"`
	Issues            []SchemaCompatibilityIssue  `json:"issues,omitempty"`
}

func (r SchemaCompatibilityReport) MismatchDetail() string {
	if len(r.Issues) == 0 {
		return ""
	}
	details := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		details = append(details, issue.Detail)
	}
	return strings.Join(details, "; ")
}

// CheckSchemaCompatibility compares the registered schema against the schema
// stored in the latest snapshot. Nil snapshot means fresh start and is always
// compatible.
func CheckSchemaCompatibility(registered SchemaRegistry, snapshot *SnapshotSchema) error {
	report := AnalyzeSchemaCompatibility(registered, snapshot)
	if report.Compatible {
		return nil
	}
	return &SchemaMismatchError{Detail: report.MismatchDetail()}
}

// AnalyzeSchemaCompatibility compares registered schema against a recovered
// snapshot and classifies the result for hosted-app migration preflights. It
// intentionally supports only migration shapes that current recovery can apply
// without rewriting rows or remapping persisted table IDs: metadata-only
// schema-version drift and appended non-unique/non-primary indexes.
func AnalyzeSchemaCompatibility(registered SchemaRegistry, snapshot *SnapshotSchema) SchemaCompatibilityReport {
	_, report := ReconcileRegistryForSnapshot(registered, snapshot)
	return report
}

// ReconcileRegistryForSnapshot returns a registry whose table IDs preserve the
// recovered snapshot's table-name mapping while assigning fresh IDs to new
// tables above the snapshot maximum. This lets hosted-app recovery accept safe
// added-table migrations without shifting persisted system-table IDs.
func ReconcileRegistryForSnapshot(registered SchemaRegistry, snapshot *SnapshotSchema) (SchemaRegistry, SchemaCompatibilityReport) {
	reconciled := reconcileRegistryForSnapshot(registered, snapshot)
	report := analyzeSchemaCompatibility(reconciled, snapshot)
	addUnreconciledRenameIssues(registered, snapshot, &report)
	return reconciled, report.finalize()
}

func analyzeSchemaCompatibility(registered SchemaRegistry, snapshot *SnapshotSchema) SchemaCompatibilityReport {
	report := SchemaCompatibilityReport{
		Compatible:        true,
		Status:            SchemaCompatibilityCompatible,
		RegisteredVersion: 0,
	}
	if registered != nil {
		report.RegisteredVersion = registered.Version()
	}
	if snapshot == nil {
		return report
	}
	report.SnapshotVersion = snapshot.Version
	if registered == nil {
		report.addIssue(SchemaCompatibilityIssueMissingRegistry, "", "", "registered schema is nil")
		return report.finalize()
	}

	if registered.Version() != snapshot.Version {
		report.addChange(SchemaCompatibilityChangeSchemaVersion, "", "", fmt.Sprintf("schema version differs: registered=%d snapshot=%d", registered.Version(), snapshot.Version))
	}

	snapshotByID := make(map[TableID]TableSchema, len(snapshot.Tables))
	for _, table := range snapshot.Tables {
		snapshotByID[table.ID] = table
	}
	registeredIDs := registered.Tables()
	for _, id := range registeredIDs {
		registeredTable, ok := registered.Table(id)
		if !ok {
			continue
		}
		if snapshotTable, ok := snapshotByID[id]; ok && registeredTable.Name != snapshotTable.Name {
			report.addIssue(SchemaCompatibilityIssueTableMismatch, registeredTable.Name, "", fmt.Sprintf("table %d name mismatch: registered=%q snapshot=%q", id, registeredTable.Name, snapshotTable.Name))
		}
	}
	for _, table := range snapshot.Tables {
		if !slices.Contains(registeredIDs, table.ID) {
			report.addIssue(SchemaCompatibilityIssueDropTable, table.Name, "", fmt.Sprintf("snapshot has unexpected table id %d (%s)", table.ID, table.Name))
		}
	}

	for _, id := range registeredIDs {
		registeredTable, ok := registered.Table(id)
		if !ok {
			report.addIssue(SchemaCompatibilityIssueTableMismatch, "", "", fmt.Sprintf("registered schema missing table id %d", id))
			continue
		}
		snapshotTable, ok := snapshotByID[id]
		if !ok {
			report.addChange(SchemaCompatibilityChangeAddTable, registeredTable.Name, "", fmt.Sprintf("table %q (id=%d) is new relative to snapshot", registeredTable.Name, id))
			continue
		}
		analyzeTableSchemaCompatibility(*registeredTable, snapshotTable, &report)
	}

	return report.finalize()
}

func reconcileRegistryForSnapshot(registered SchemaRegistry, snapshot *SnapshotSchema) SchemaRegistry {
	if registered == nil || snapshot == nil {
		return registered
	}
	snapshotByName := make(map[string]TableSchema, len(snapshot.Tables))
	var maxID TableID
	for _, table := range snapshot.Tables {
		snapshotByName[table.Name] = table
		if table.ID >= maxID {
			maxID = table.ID
		}
	}

	tableIDs := registered.Tables()
	tables := make([]TableSchema, 0, len(tableIDs))
	nextID := maxID + 1
	for _, tableID := range tableIDs {
		table, ok := registered.Table(tableID)
		if !ok {
			continue
		}
		current := cloneTableSchema(*table)
		if snapshotTable, ok := snapshotByName[current.Name]; ok {
			current.ID = snapshotTable.ID
		} else {
			current.ID = nextID
			nextID++
		}
		tables = append(tables, current)
	}
	return newRemappedRegistry(registered, tables)
}

func addUnreconciledRenameIssues(registered SchemaRegistry, snapshot *SnapshotSchema, report *SchemaCompatibilityReport) {
	if registered == nil || snapshot == nil || report == nil {
		return
	}
	snapshotByID := make(map[TableID]TableSchema, len(snapshot.Tables))
	snapshotNames := make(map[string]struct{}, len(snapshot.Tables))
	for _, table := range snapshot.Tables {
		snapshotByID[table.ID] = table
		snapshotNames[table.Name] = struct{}{}
	}
	registeredNames := make(map[string]struct{}, len(registered.Tables()))
	for _, tableID := range registered.Tables() {
		table, ok := registered.Table(tableID)
		if !ok {
			continue
		}
		registeredNames[table.Name] = struct{}{}
	}
	for _, tableID := range registered.Tables() {
		registeredTable, ok := registered.Table(tableID)
		if !ok {
			continue
		}
		snapshotTable, ok := snapshotByID[tableID]
		if !ok || registeredTable.Name == snapshotTable.Name {
			continue
		}
		_, registeredNameInSnapshot := snapshotNames[registeredTable.Name]
		_, snapshotNameInRegistered := registeredNames[snapshotTable.Name]
		if registeredNameInSnapshot || snapshotNameInRegistered {
			continue
		}
		report.addIssue(SchemaCompatibilityIssueTableMismatch, registeredTable.Name, "", fmt.Sprintf("table %d name mismatch: registered=%q snapshot=%q", tableID, registeredTable.Name, snapshotTable.Name))
		analyzeTableSchemaCompatibility(*registeredTable, snapshotTable, report)
	}
}

func (r *SchemaCompatibilityReport) addChange(kind SchemaCompatibilityChangeKind, table, index, detail string) {
	r.Changes = append(r.Changes, SchemaCompatibilityChange{
		Kind:   kind,
		Table:  table,
		Index:  index,
		Detail: detail,
	})
}

func (r *SchemaCompatibilityReport) addIssue(kind SchemaCompatibilityIssueKind, table, index, detail string) {
	r.Issues = append(r.Issues, SchemaCompatibilityIssue{
		Kind:   kind,
		Table:  table,
		Index:  index,
		Detail: detail,
	})
}

func (r SchemaCompatibilityReport) finalize() SchemaCompatibilityReport {
	if len(r.Issues) > 0 {
		r.Compatible = false
		r.Status = SchemaCompatibilityBlocked
		return r
	}
	r.Compatible = true
	if len(r.Changes) > 0 {
		r.Status = SchemaCompatibilityAdditive
	} else {
		r.Status = SchemaCompatibilityCompatible
	}
	return r
}

func analyzeTableSchemaCompatibility(registered, snapshot TableSchema, report *SchemaCompatibilityReport) {
	if registered.Name != snapshot.Name {
		report.addIssue(SchemaCompatibilityIssueTableMismatch, registered.Name, "", fmt.Sprintf("table %d name mismatch: registered=%q snapshot=%q", registered.ID, registered.Name, snapshot.Name))
	}
	if registered.IsEvent != snapshot.IsEvent {
		report.addIssue(SchemaCompatibilityIssueTableMismatch, registered.Name, "", fmt.Sprintf("table %q kind mismatch: registered_event=%t snapshot_event=%t", registered.Name, registered.IsEvent, snapshot.IsEvent))
	}
	if len(registered.Columns) != len(snapshot.Columns) {
		report.addIssue(SchemaCompatibilityIssueColumnMismatch, registered.Name, "", fmt.Sprintf("table %q column count mismatch: registered=%d snapshot=%d; row-shape changes require an app-owned migration", registered.Name, len(registered.Columns), len(snapshot.Columns)))
	} else {
		for i := range registered.Columns {
			rc := registered.Columns[i]
			sc := snapshot.Columns[i]
			if rc.Index != sc.Index {
				report.addIssue(SchemaCompatibilityIssueColumnMismatch, registered.Name, "", fmt.Sprintf("table %q column %d index mismatch: registered=%d snapshot=%d", registered.Name, i, rc.Index, sc.Index))
			}
			if rc.Name != sc.Name {
				report.addIssue(SchemaCompatibilityIssueColumnMismatch, registered.Name, "", fmt.Sprintf("table %q column %d name mismatch: registered=%q snapshot=%q", registered.Name, i, rc.Name, sc.Name))
			}
			if rc.Type != sc.Type {
				report.addIssue(SchemaCompatibilityIssueColumnMismatch, registered.Name, "", fmt.Sprintf("table %q column %q type mismatch: registered=%v snapshot=%v", registered.Name, rc.Name, rc.Type, sc.Type))
			}
			if rc.Nullable != sc.Nullable {
				report.addIssue(SchemaCompatibilityIssueColumnMismatch, registered.Name, "", fmt.Sprintf("table %q column %q nullable mismatch: registered=%t snapshot=%t", registered.Name, rc.Name, rc.Nullable, sc.Nullable))
			}
			if rc.AutoIncrement != sc.AutoIncrement {
				report.addIssue(SchemaCompatibilityIssueColumnMismatch, registered.Name, "", fmt.Sprintf("table %q column %q auto_increment mismatch: registered=%t snapshot=%t", registered.Name, rc.Name, rc.AutoIncrement, sc.AutoIncrement))
			}
		}
	}
	analyzeIndexCompatibility(registered, snapshot, report)
}

func analyzeIndexCompatibility(registered, snapshot TableSchema, report *SchemaCompatibilityReport) {
	common := min(len(registered.Indexes), len(snapshot.Indexes))
	for i := range common {
		ri := registered.Indexes[i]
		si := snapshot.Indexes[i]
		if ri.Name != si.Name {
			report.addIssue(SchemaCompatibilityIssueIndexMismatch, registered.Name, ri.Name, fmt.Sprintf("table %q index %d name mismatch: registered=%q snapshot=%q", registered.Name, i, ri.Name, si.Name))
		}
		if !slices.Equal(ri.Columns, si.Columns) {
			report.addIssue(SchemaCompatibilityIssueIndexMismatch, registered.Name, ri.Name, fmt.Sprintf("table %q index %q columns mismatch: registered=%v snapshot=%v", registered.Name, ri.Name, ri.Columns, si.Columns))
		}
		if ri.Unique != si.Unique {
			report.addIssue(SchemaCompatibilityIssueIndexMismatch, registered.Name, ri.Name, fmt.Sprintf("table %q index %q unique mismatch: registered=%t snapshot=%t", registered.Name, ri.Name, ri.Unique, si.Unique))
		}
		if ri.Primary != si.Primary {
			report.addIssue(SchemaCompatibilityIssueIndexMismatch, registered.Name, ri.Name, fmt.Sprintf("table %q index %q primary mismatch: registered=%t snapshot=%t", registered.Name, ri.Name, ri.Primary, si.Primary))
		}
	}
	if len(snapshot.Indexes) > len(registered.Indexes) {
		for _, idx := range snapshot.Indexes[len(registered.Indexes):] {
			report.addIssue(SchemaCompatibilityIssueIndexMismatch, registered.Name, idx.Name, fmt.Sprintf("table %q snapshot has extra index %q; dropping indexes is not an automatic hosted-app migration", registered.Name, idx.Name))
		}
		return
	}
	for _, idx := range registered.Indexes[len(snapshot.Indexes):] {
		if idx.Unique || idx.Primary {
			report.addIssue(SchemaCompatibilityIssueIndexMismatch, registered.Name, idx.Name, fmt.Sprintf("table %q new index %q is unique or primary; constraint additions require app-owned validation before migration", registered.Name, idx.Name))
			continue
		}
		report.addChange(SchemaCompatibilityChangeAddIndex, registered.Name, idx.Name, fmt.Sprintf("table %q index %q is new relative to snapshot", registered.Name, idx.Name))
	}
}

// Start performs runtime initialization, including schema compatibility checks
// against snapshot metadata when startup recovery has supplied it.
func (e *Engine) Start(_ context.Context) error {
	return CheckSchemaCompatibility(e.registry, e.opts.StartupSnapshotSchema)
}
