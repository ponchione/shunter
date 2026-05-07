package contractdiff

import (
	"fmt"
	"sort"
	"strings"

	shunter "github.com/ponchione/shunter"
)

type WarningCode string

const (
	WarningMissingMigrationMetadata          WarningCode = "missing-migration-metadata"
	WarningRiskyChangeDeclaredCompatible     WarningCode = "risky-change-declared-compatible"
	WarningBreakingDeclaredForAdditiveChange WarningCode = "breaking-declared-for-additive-change"
	WarningMissingPreviousVersion            WarningCode = "missing-previous-version"
)

type PolicyOptions struct {
	RequirePreviousVersion bool
	Strict                 bool
}

type PolicyWarning struct {
	Code    WarningCode
	Surface Surface
	Name    string
	Detail  string
}

type PolicyResult struct {
	Warnings []PolicyWarning
	Failed   bool
}

func CheckPolicy(report Report, current shunter.ModuleContract, opts PolicyOptions) PolicyResult {
	var result PolicyResult
	if opts.RequirePreviousVersion && current.Migrations.Module.PreviousVersion == "" {
		name := current.Module.Name
		if name == "" {
			name = "module"
		}
		result.addWarning(WarningMissingPreviousVersion, SurfaceModule, name, "module migration metadata is missing previous_version")
	}

	for _, change := range report.Changes {
		if change.Kind != ChangeKindAdditive && change.Kind != ChangeKindBreaking {
			continue
		}
		metadata, ok := migrationMetadataForChange(current.Migrations, change)
		if !ok || !migrationMetadataPresent(metadata) {
			result.addWarning(
				WarningMissingMigrationMetadata,
				change.Surface,
				change.Name,
				fmt.Sprintf("%s change has no migration metadata", change.Kind),
			)
			continue
		}
		if change.Kind == ChangeKindBreaking && metadata.Compatibility == shunter.MigrationCompatibilityCompatible {
			result.addWarning(
				WarningRiskyChangeDeclaredCompatible,
				change.Surface,
				change.Name,
				"breaking inferred change is declared compatible",
			)
		}
		if change.Kind == ChangeKindAdditive && metadata.Compatibility == shunter.MigrationCompatibilityBreaking {
			result.addWarning(
				WarningBreakingDeclaredForAdditiveChange,
				change.Surface,
				change.Name,
				"additive inferred change is declared breaking",
			)
		}
	}

	sortPolicyWarnings(result.Warnings)
	result.Failed = opts.Strict && len(result.Warnings) > 0
	return result
}

func migrationMetadataForChange(migrations shunter.MigrationContract, change Change) (shunter.MigrationMetadata, bool) {
	switch change.Surface {
	case SurfaceTable, SurfaceTableReadPolicy:
		return migrationMetadataForNamedSurface(migrations, shunter.MigrationSurfaceTable, change.Name, change.Kind)
	case SurfaceColumn, SurfaceIndex:
		tableName := change.Name
		if idx := strings.IndexByte(change.Name, '.'); idx >= 0 {
			tableName = change.Name[:idx]
		}
		return findMigrationDeclaration(migrations.Declarations, shunter.MigrationSurfaceTable, tableName)
	case SurfaceQuery:
		return migrationMetadataForNamedSurface(migrations, shunter.MigrationSurfaceQuery, change.Name, change.Kind)
	case SurfaceView:
		return migrationMetadataForNamedSurface(migrations, shunter.MigrationSurfaceView, change.Name, change.Kind)
	case SurfacePermission:
		surface, name, ok := strings.Cut(change.Name, ".")
		if !ok {
			return shunter.MigrationMetadata{}, false
		}
		switch surface {
		case "reducer":
			return migrations.Module, true
		case shunter.MigrationSurfaceQuery, shunter.MigrationSurfaceView:
			return migrationMetadataForNamedSurface(migrations, surface, name, change.Kind)
		default:
			return shunter.MigrationMetadata{}, false
		}
	case SurfaceCodegen, SurfaceVisibilityFilter:
		return migrations.Module, true
	case SurfaceReducer, SurfaceContract, SurfaceModule, SurfaceSchema:
		return migrations.Module, true
	default:
		return shunter.MigrationMetadata{}, false
	}
}

func migrationMetadataForNamedSurface(migrations shunter.MigrationContract, surface, name string, kind ChangeKind) (shunter.MigrationMetadata, bool) {
	if metadata, ok := findMigrationDeclaration(migrations.Declarations, surface, name); ok {
		return metadata, true
	}
	if kind == ChangeKindBreaking {
		return migrations.Module, true
	}
	return shunter.MigrationMetadata{}, false
}

func findMigrationDeclaration(declarations []shunter.MigrationContractDeclaration, surface, name string) (shunter.MigrationMetadata, bool) {
	for _, declaration := range declarations {
		if declaration.Surface == surface && declaration.Name == name {
			return declaration.Metadata, true
		}
	}
	return shunter.MigrationMetadata{}, false
}

func migrationMetadataPresent(metadata shunter.MigrationMetadata) bool {
	return metadata.ModuleVersion != "" ||
		metadata.SchemaVersion != 0 ||
		metadata.ContractVersion != 0 ||
		metadata.PreviousVersion != "" ||
		metadata.Compatibility != "" ||
		len(metadata.Classifications) > 0 ||
		metadata.Notes != ""
}

func (r *PolicyResult) addWarning(code WarningCode, surface Surface, name, detail string) {
	r.Warnings = append(r.Warnings, PolicyWarning{
		Code:    code,
		Surface: surface,
		Name:    name,
		Detail:  detail,
	})
}

func sortPolicyWarnings(warnings []PolicyWarning) {
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
