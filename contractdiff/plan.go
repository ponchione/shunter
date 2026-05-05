package contractdiff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	shunter "github.com/ponchione/shunter"
)

const (
	WarningMigrationMetadataModuleVersionMismatch   WarningCode = "migration-metadata-module-version-mismatch"
	WarningMigrationMetadataSchemaVersionMismatch   WarningCode = "migration-metadata-schema-version-mismatch"
	WarningMigrationMetadataContractVersionMismatch WarningCode = "migration-metadata-contract-version-mismatch"
	WarningMigrationMetadataPreviousVersionMismatch WarningCode = "migration-metadata-previous-version-mismatch"
	WarningSchemaVersionRegressed                   WarningCode = "schema-version-regressed"
	WarningContractVersionRegressed                 WarningCode = "contract-version-regressed"
	WarningContractFormatMismatch                   WarningCode = "contract-format-mismatch"
	WarningCodegenContractVersionMismatch           WarningCode = "codegen-contract-version-mismatch"
)

type PlanSeverity string

const (
	PlanSeverityReview   PlanSeverity = "review"
	PlanSeverityWarning  PlanSeverity = "warning"
	PlanSeverityBlocking PlanSeverity = "blocking"
)

type PlanAction string

const (
	PlanActionReviewRequired       PlanAction = "review-required"
	PlanActionManualReviewNeeded   PlanAction = "manual-review-needed"
	PlanActionExecutionUnsupported PlanAction = "execution-unsupported"
)

type PlanGuidanceCode string

const (
	PlanGuidanceBackupRestore PlanGuidanceCode = "backup-restore"
)

type PlanOptions struct {
	Policy            PolicyOptions
	ValidateContracts bool
}

type MigrationPlan struct {
	Summary  PlanSummary    `json:"summary"`
	Entries  []PlanEntry    `json:"entries"`
	Warnings []PlanWarning  `json:"warnings"`
	Guidance []PlanGuidance `json:"guidance"`
}

type PlanSummary struct {
	TotalChanges         int  `json:"total_changes"`
	Additive             int  `json:"additive"`
	Breaking             int  `json:"breaking"`
	MetadataOnly         int  `json:"metadata_only"`
	ReviewRequired       int  `json:"review_required"`
	ManualReviewNeeded   int  `json:"manual_review_needed"`
	DataRewriteNeeded    int  `json:"data_rewrite_needed"`
	ExecutionUnsupported int  `json:"execution_unsupported"`
	Blocking             int  `json:"blocking"`
	BackupRecommended    bool `json:"backup_recommended"`
	Warnings             int  `json:"warnings"`
	PolicyFailed         bool `json:"policy_failed"`
}

type PlanEntry struct {
	Kind              ChangeKind                        `json:"kind"`
	Surface           Surface                           `json:"surface"`
	Name              string                            `json:"name"`
	Detail            string                            `json:"detail"`
	Severity          PlanSeverity                      `json:"severity"`
	Action            PlanAction                        `json:"action"`
	MigrationMetadata *shunter.MigrationMetadata        `json:"migration_metadata,omitempty"`
	Classifications   []shunter.MigrationClassification `json:"classifications"`
}

type PlanWarning struct {
	Code    WarningCode `json:"code"`
	Surface Surface     `json:"surface"`
	Name    string      `json:"name"`
	Detail  string      `json:"detail"`
}

type PlanGuidance struct {
	Code   PlanGuidanceCode `json:"code"`
	Detail string           `json:"detail"`
}

func PlanJSON(oldData, currentData []byte, opts PlanOptions) (MigrationPlan, error) {
	old, err := decodeContractJSON("previous", oldData)
	if err != nil {
		return MigrationPlan{}, err
	}
	current, err := decodeContractJSON("current", currentData)
	if err != nil {
		return MigrationPlan{}, err
	}
	return Plan(old, current, opts), nil
}

func Plan(old, current shunter.ModuleContract, opts PlanOptions) MigrationPlan {
	report := Compare(old, current)
	policy := CheckPolicy(report, current, opts.Policy)

	plan := MigrationPlan{
		Entries:  make([]PlanEntry, 0, len(report.Changes)),
		Warnings: make([]PlanWarning, 0, len(policy.Warnings)),
	}
	for _, change := range report.Changes {
		entry := newPlanEntry(current.Migrations, change)
		plan.Entries = append(plan.Entries, entry)
		updatePlanSummaryForEntry(&plan.Summary, entry)
	}
	for _, warning := range policy.Warnings {
		plan.Warnings = append(plan.Warnings, PlanWarning(warning))
	}
	if opts.ValidateContracts {
		plan.Warnings = append(plan.Warnings, validatePlanContracts(old, current)...)
	}
	sortPlanEntries(plan.Entries)
	sortPlanWarnings(plan.Warnings)
	plan.Summary.TotalChanges = len(plan.Entries)
	plan.Summary.Warnings = len(plan.Warnings)
	plan.Summary.PolicyFailed = policy.Failed
	plan.Summary.BackupRecommended = planBackupRecommended(plan.Entries)
	if plan.Summary.BackupRecommended {
		plan.Guidance = append(plan.Guidance, backupRestorePlanGuidance())
	}
	sortPlanGuidance(plan.Guidance)
	return normalizeMigrationPlan(plan)
}

func (p MigrationPlan) Text() string {
	p = normalizeMigrationPlan(p)
	if len(p.Entries) == 0 && len(p.Warnings) == 0 && len(p.Guidance) == 0 {
		return "No migration plan changes.\n"
	}
	var b strings.Builder
	for _, entry := range p.Entries {
		fmt.Fprintf(&b, "%s %s %s %s %s: %s", entry.Severity, entry.Action, entry.Kind, entry.Surface, entry.Name, entry.Detail)
		if len(entry.Classifications) > 0 {
			fmt.Fprintf(&b, " [%s]", migrationClassificationsText(entry.Classifications))
		}
		b.WriteByte('\n')
	}
	for _, warning := range p.Warnings {
		fmt.Fprintf(&b, "warning %s %s %s: %s\n", warning.Code, warning.Surface, warning.Name, warning.Detail)
	}
	for _, guidance := range p.Guidance {
		fmt.Fprintf(&b, "guidance %s: %s\n", guidance.Code, guidance.Detail)
	}
	return b.String()
}

func (p MigrationPlan) MarshalCanonicalJSON() ([]byte, error) {
	data, err := json.MarshalIndent(normalizeMigrationPlan(p), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func newPlanEntry(migrations shunter.MigrationContract, change Change) PlanEntry {
	metadata, ok := migrationMetadataForPlanChange(migrations, change)
	metadataCopy := copyPlanMigrationMetadata(metadata)
	var metadataPtr *shunter.MigrationMetadata
	var classifications []shunter.MigrationClassification
	if ok && migrationMetadataPresent(metadataCopy) {
		metadataPtr = &metadataCopy
		classifications = copyPlanClassifications(metadataCopy.Classifications)
	}
	classifications = appendInferredPlanClassifications(change, classifications)
	severity, action := actionForPlanChange(change, classifications)
	if change.Surface == SurfaceMigrationMetadata {
		severity = PlanSeverityReview
		action = PlanActionReviewRequired
	}
	return PlanEntry{
		Kind:              change.Kind,
		Surface:           change.Surface,
		Name:              change.Name,
		Detail:            change.Detail,
		Severity:          severity,
		Action:            action,
		MigrationMetadata: metadataPtr,
		Classifications:   normalizePlanClassifications(classifications),
	}
}

func appendInferredPlanClassifications(change Change, classifications []shunter.MigrationClassification) []shunter.MigrationClassification {
	if !needsPolicyManualReviewClassification(change) ||
		hasPlanClassification(classifications, shunter.MigrationClassificationManualReviewNeeded) {
		return classifications
	}
	out := copyPlanClassifications(classifications)
	return append(out, shunter.MigrationClassificationManualReviewNeeded)
}

func needsPolicyManualReviewClassification(change Change) bool {
	if change.Kind != ChangeKindAdditive {
		return false
	}
	switch change.Surface {
	case SurfaceTableReadPolicy:
		return true
	case SurfacePermission:
		surface, _, ok := strings.Cut(change.Name, ".")
		return ok && (surface == shunter.MigrationSurfaceQuery || surface == shunter.MigrationSurfaceView)
	default:
		return false
	}
}

func actionForPlanChange(change Change, classifications []shunter.MigrationClassification) (PlanSeverity, PlanAction) {
	if hasPlanClassification(classifications, shunter.MigrationClassificationDataRewriteNeeded) {
		return PlanSeverityBlocking, PlanActionExecutionUnsupported
	}
	if change.Kind == ChangeKindBreaking {
		return PlanSeverityBlocking, PlanActionManualReviewNeeded
	}
	if hasPlanClassification(classifications, shunter.MigrationClassificationManualReviewNeeded) {
		return PlanSeverityWarning, PlanActionManualReviewNeeded
	}
	return PlanSeverityReview, PlanActionReviewRequired
}

func migrationMetadataForPlanChange(migrations shunter.MigrationContract, change Change) (shunter.MigrationMetadata, bool) {
	if change.Surface != SurfaceMigrationMetadata {
		return migrationMetadataForChange(migrations, change)
	}
	if change.Name == "module" {
		return migrations.Module, true
	}
	surface, name, ok := strings.Cut(change.Name, ".")
	if !ok {
		return shunter.MigrationMetadata{}, false
	}
	return findMigrationDeclaration(migrations.Declarations, surface, name)
}

func updatePlanSummaryForEntry(summary *PlanSummary, entry PlanEntry) {
	switch entry.Kind {
	case ChangeKindAdditive:
		summary.Additive++
	case ChangeKindBreaking:
		summary.Breaking++
	case ChangeKindMetadata:
		summary.MetadataOnly++
	}
	switch entry.Action {
	case PlanActionReviewRequired:
		summary.ReviewRequired++
	case PlanActionManualReviewNeeded:
		summary.ManualReviewNeeded++
	case PlanActionExecutionUnsupported:
		summary.ExecutionUnsupported++
	}
	if entry.Action == PlanActionExecutionUnsupported {
		summary.DataRewriteNeeded++
	}
	if entry.Severity == PlanSeverityBlocking {
		summary.Blocking++
	}
}

func planBackupRecommended(entries []PlanEntry) bool {
	for _, entry := range entries {
		if entry.Severity == PlanSeverityBlocking ||
			hasPlanClassification(entry.Classifications, shunter.MigrationClassificationDataRewriteNeeded) {
			return true
		}
	}
	return false
}

func backupRestorePlanGuidance() PlanGuidance {
	return PlanGuidance{
		Code:   PlanGuidanceBackupRestore,
		Detail: "Stop the runtime before applying this plan to a durable DataDir; back up the complete DataDir with shunter.BackupDataDir or shunter backup, and keep that backup available for restore with shunter.RestoreDataDir or shunter restore.",
	}
}

func validatePlanContracts(old, current shunter.ModuleContract) []PlanWarning {
	var warnings []PlanWarning
	moduleName := nonEmptyName(current.Module.Name, old.Module.Name)
	warnings = appendMigrationMetadataConsistencyWarnings(warnings, old, current, current.Migrations.Module, migrationMetadataWarningScope{
		Label:           "module",
		ModuleSurface:   SurfaceModule,
		ModuleName:      moduleName,
		SchemaSurface:   SurfaceSchema,
		SchemaName:      "schema",
		ContractSurface: SurfaceContract,
		ContractName:    "contract",
		PreviousSurface: SurfaceModule,
		PreviousName:    moduleName,
	})
	for _, declaration := range current.Migrations.Declarations {
		surface, ok := migrationDeclarationPlanSurface(declaration.Surface)
		if !ok {
			continue
		}
		warnings = appendMigrationMetadataConsistencyWarnings(warnings, old, current, declaration.Metadata, migrationMetadataWarningScope{
			Label:           declaration.Surface + " " + declaration.Name,
			ModuleSurface:   surface,
			ModuleName:      declaration.Name,
			SchemaSurface:   surface,
			SchemaName:      declaration.Name,
			ContractSurface: surface,
			ContractName:    declaration.Name,
			PreviousSurface: surface,
			PreviousName:    declaration.Name,
		})
	}
	if current.Schema.Version < old.Schema.Version {
		warnings = append(warnings, PlanWarning{
			Code:    WarningSchemaVersionRegressed,
			Surface: SurfaceSchema,
			Name:    "schema",
			Detail:  fmt.Sprintf("schema version regressed from %d to %d", old.Schema.Version, current.Schema.Version),
		})
	}
	if current.ContractVersion < old.ContractVersion {
		warnings = append(warnings, PlanWarning{
			Code:    WarningContractVersionRegressed,
			Surface: SurfaceContract,
			Name:    "contract",
			Detail:  fmt.Sprintf("contract version regressed from %d to %d", old.ContractVersion, current.ContractVersion),
		})
	}
	if current.Codegen.ContractFormat != "" && current.Codegen.ContractFormat != shunter.ModuleContractFormat {
		warnings = append(warnings, PlanWarning{
			Code:    WarningContractFormatMismatch,
			Surface: SurfaceContract,
			Name:    "contract",
			Detail:  fmt.Sprintf("codegen contract format %q does not match %q", current.Codegen.ContractFormat, shunter.ModuleContractFormat),
		})
	}
	if current.Codegen.ContractVersion != 0 && current.Codegen.ContractVersion != current.ContractVersion {
		warnings = append(warnings, PlanWarning{
			Code:    WarningCodegenContractVersionMismatch,
			Surface: SurfaceContract,
			Name:    "contract",
			Detail:  fmt.Sprintf("codegen contract version %d does not match current contract version %d", current.Codegen.ContractVersion, current.ContractVersion),
		})
	}
	return warnings
}

type migrationMetadataWarningScope struct {
	Label           string
	ModuleSurface   Surface
	ModuleName      string
	SchemaSurface   Surface
	SchemaName      string
	ContractSurface Surface
	ContractName    string
	PreviousSurface Surface
	PreviousName    string
}

func appendMigrationMetadataConsistencyWarnings(warnings []PlanWarning, old, current shunter.ModuleContract, metadata shunter.MigrationMetadata, scope migrationMetadataWarningScope) []PlanWarning {
	if metadata.ModuleVersion != "" && current.Module.Version != "" && metadata.ModuleVersion != current.Module.Version {
		warnings = append(warnings, PlanWarning{
			Code:    WarningMigrationMetadataModuleVersionMismatch,
			Surface: scope.ModuleSurface,
			Name:    scope.ModuleName,
			Detail:  fmt.Sprintf("%s migration metadata version %q does not match current module version %q", scope.Label, metadata.ModuleVersion, current.Module.Version),
		})
	}
	if metadata.SchemaVersion != 0 && metadata.SchemaVersion != current.Schema.Version {
		warnings = append(warnings, PlanWarning{
			Code:    WarningMigrationMetadataSchemaVersionMismatch,
			Surface: scope.SchemaSurface,
			Name:    scope.SchemaName,
			Detail:  fmt.Sprintf("%s migration metadata schema version %d does not match current schema version %d", scope.Label, metadata.SchemaVersion, current.Schema.Version),
		})
	}
	if metadata.ContractVersion != 0 && metadata.ContractVersion != current.ContractVersion {
		warnings = append(warnings, PlanWarning{
			Code:    WarningMigrationMetadataContractVersionMismatch,
			Surface: scope.ContractSurface,
			Name:    scope.ContractName,
			Detail:  fmt.Sprintf("%s migration metadata contract version %d does not match current contract version %d", scope.Label, metadata.ContractVersion, current.ContractVersion),
		})
	}
	if metadata.PreviousVersion != "" && old.Module.Version != "" && metadata.PreviousVersion != old.Module.Version {
		warnings = append(warnings, PlanWarning{
			Code:    WarningMigrationMetadataPreviousVersionMismatch,
			Surface: scope.PreviousSurface,
			Name:    scope.PreviousName,
			Detail:  fmt.Sprintf("%s migration metadata previous_version %q does not match previous module version %q", scope.Label, metadata.PreviousVersion, old.Module.Version),
		})
	}
	return warnings
}

func migrationDeclarationPlanSurface(surface string) (Surface, bool) {
	switch surface {
	case shunter.MigrationSurfaceTable:
		return SurfaceTable, true
	case shunter.MigrationSurfaceQuery:
		return SurfaceQuery, true
	case shunter.MigrationSurfaceView:
		return SurfaceView, true
	default:
		return "", false
	}
}

func normalizeMigrationPlan(plan MigrationPlan) MigrationPlan {
	if plan.Entries == nil {
		plan.Entries = []PlanEntry{}
	}
	for i := range plan.Entries {
		plan.Entries[i].Classifications = normalizePlanClassifications(plan.Entries[i].Classifications)
		if plan.Entries[i].MigrationMetadata != nil {
			metadata := copyPlanMigrationMetadata(*plan.Entries[i].MigrationMetadata)
			plan.Entries[i].MigrationMetadata = &metadata
		}
	}
	if plan.Warnings == nil {
		plan.Warnings = []PlanWarning{}
	}
	if plan.Guidance == nil {
		plan.Guidance = []PlanGuidance{}
	}
	return plan
}

func copyPlanMigrationMetadata(in shunter.MigrationMetadata) shunter.MigrationMetadata {
	return shunter.MigrationMetadata{
		ModuleVersion:   in.ModuleVersion,
		SchemaVersion:   in.SchemaVersion,
		ContractVersion: in.ContractVersion,
		PreviousVersion: in.PreviousVersion,
		Compatibility:   in.Compatibility,
		Classifications: normalizePlanClassifications(in.Classifications),
		Notes:           in.Notes,
	}
}

func copyPlanClassifications(in []shunter.MigrationClassification) []shunter.MigrationClassification {
	if len(in) == 0 {
		return nil
	}
	out := make([]shunter.MigrationClassification, len(in))
	copy(out, in)
	return out
}

func normalizePlanClassifications(in []shunter.MigrationClassification) []shunter.MigrationClassification {
	if len(in) == 0 {
		return []shunter.MigrationClassification{}
	}
	return copyPlanClassifications(in)
}

func hasPlanClassification(classifications []shunter.MigrationClassification, want shunter.MigrationClassification) bool {
	for _, classification := range classifications {
		if classification == want {
			return true
		}
	}
	return false
}

func migrationClassificationsText(classifications []shunter.MigrationClassification) string {
	parts := make([]string, len(classifications))
	for i, classification := range classifications {
		parts[i] = string(classification)
	}
	return strings.Join(parts, ",")
}

func sortPlanEntries(entries []PlanEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Detail < b.Detail
	})
}

func sortPlanWarnings(warnings []PlanWarning) {
	sort.SliceStable(warnings, func(i, j int) bool {
		a, b := warnings[i], warnings[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Detail < b.Detail
	})
}

func sortPlanGuidance(guidance []PlanGuidance) {
	sort.SliceStable(guidance, func(i, j int) bool {
		a, b := guidance[i], guidance[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Detail < b.Detail
	})
}
