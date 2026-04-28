package codegen

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	shunter "github.com/ponchione/shunter"
)

const (
	// LanguageTypeScript is the first supported client binding target.
	LanguageTypeScript = "typescript"
)

var (
	// ErrUnsupportedLanguage reports a requested generator target that does not exist.
	ErrUnsupportedLanguage = errors.New("unsupported language")

	// ErrInvalidContract reports canonical contract input that cannot be used for codegen.
	ErrInvalidContract = errors.New("invalid module contract")
)

// Options configures client binding generation.
type Options struct {
	Language string
}

// GenerateFromJSON decodes canonical ModuleContract JSON and generates bindings.
func GenerateFromJSON(data []byte, opts Options) ([]byte, error) {
	var contract shunter.ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return nil, fmt.Errorf("%w: decode JSON: %v", ErrInvalidContract, err)
	}
	return Generate(contract, opts)
}

// Generate emits client bindings from a detached ModuleContract.
func Generate(contract shunter.ModuleContract, opts Options) ([]byte, error) {
	lang := strings.ToLower(strings.TrimSpace(opts.Language))
	switch lang {
	case LanguageTypeScript:
		return GenerateTypeScript(contract)
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedLanguage, opts.Language)
	}
}

// GenerateTypeScript emits deterministic TypeScript bindings from a ModuleContract.
func GenerateTypeScript(contract shunter.ModuleContract) ([]byte, error) {
	if err := validateContract(contract); err != nil {
		return nil, err
	}
	return generateTypeScript(contract)
}

func validateContract(contract shunter.ModuleContract) error {
	if contract.ContractVersion != shunter.ModuleContractVersion {
		return fmt.Errorf("%w: contract_version = %d, want %d", ErrInvalidContract, contract.ContractVersion, shunter.ModuleContractVersion)
	}
	if contract.Codegen.ContractFormat != shunter.ModuleContractFormat {
		return fmt.Errorf("%w: codegen.contract_format = %q, want %q", ErrInvalidContract, contract.Codegen.ContractFormat, shunter.ModuleContractFormat)
	}
	if contract.Codegen.ContractVersion != shunter.ModuleContractVersion {
		return fmt.Errorf("%w: codegen.contract_version = %d, want %d", ErrInvalidContract, contract.Codegen.ContractVersion, shunter.ModuleContractVersion)
	}
	for _, table := range contract.Schema.Tables {
		if strings.TrimSpace(table.Name) == "" {
			return fmt.Errorf("%w: table name must not be empty", ErrInvalidContract)
		}
		for _, column := range table.Columns {
			if strings.TrimSpace(column.Name) == "" {
				return fmt.Errorf("%w: column name must not be empty in table %q", ErrInvalidContract, table.Name)
			}
			if _, err := typeScriptColumnType(column.Type); err != nil {
				return fmt.Errorf("%w: table %q column %q: %v", ErrInvalidContract, table.Name, column.Name, err)
			}
		}
	}
	for _, reducer := range contract.Schema.Reducers {
		if strings.TrimSpace(reducer.Name) == "" {
			return fmt.Errorf("%w: reducer name must not be empty", ErrInvalidContract)
		}
	}
	for _, query := range contract.Queries {
		if strings.TrimSpace(query.Name) == "" {
			return fmt.Errorf("%w: query name must not be empty", ErrInvalidContract)
		}
	}
	for _, view := range contract.Views {
		if strings.TrimSpace(view.Name) == "" {
			return fmt.Errorf("%w: view name must not be empty", ErrInvalidContract)
		}
	}
	if err := validatePermissionDeclarations("reducer", contract.Permissions.Reducers); err != nil {
		return err
	}
	if err := validatePermissionDeclarations("query", contract.Permissions.Queries); err != nil {
		return err
	}
	if err := validatePermissionDeclarations("view", contract.Permissions.Views); err != nil {
		return err
	}
	for _, declaration := range contract.ReadModel.Declarations {
		if declaration.Surface != shunter.ReadModelSurfaceQuery && declaration.Surface != shunter.ReadModelSurfaceView {
			return fmt.Errorf("%w: read model surface %q is invalid", ErrInvalidContract, declaration.Surface)
		}
		if strings.TrimSpace(declaration.Name) == "" {
			return fmt.Errorf("%w: read model %s name must not be empty", ErrInvalidContract, declaration.Surface)
		}
		for _, table := range declaration.Tables {
			if strings.TrimSpace(table) == "" {
				return fmt.Errorf("%w: read model %s %q table must not be empty", ErrInvalidContract, declaration.Surface, declaration.Name)
			}
		}
		for _, tag := range declaration.Tags {
			if strings.TrimSpace(tag) == "" {
				return fmt.Errorf("%w: read model %s %q tag must not be empty", ErrInvalidContract, declaration.Surface, declaration.Name)
			}
		}
	}
	return nil
}

func validatePermissionDeclarations(surface string, declarations []shunter.PermissionContractDeclaration) error {
	for _, declaration := range declarations {
		if strings.TrimSpace(declaration.Name) == "" {
			return fmt.Errorf("%w: permission %s name must not be empty", ErrInvalidContract, surface)
		}
		for _, required := range declaration.Required {
			if strings.TrimSpace(required) == "" {
				return fmt.Errorf("%w: permission %s %q requirement must not be empty", ErrInvalidContract, surface, declaration.Name)
			}
		}
	}
	return nil
}
