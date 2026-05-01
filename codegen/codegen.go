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
	if err := ValidateOptions(opts); err != nil {
		return nil, err
	}
	var contract shunter.ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return nil, fmt.Errorf("%w: decode JSON: %v", ErrInvalidContract, err)
	}
	return Generate(contract, opts)
}

// Generate emits client bindings from a detached ModuleContract.
func Generate(contract shunter.ModuleContract, opts Options) ([]byte, error) {
	if err := ValidateOptions(opts); err != nil {
		return nil, err
	}
	switch normalizedLanguage(opts) {
	case LanguageTypeScript:
		return GenerateTypeScript(contract)
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedLanguage, opts.Language)
	}
}

// ValidateOptions rejects unsupported generator options before input decoding or file I/O.
func ValidateOptions(opts Options) error {
	switch normalizedLanguage(opts) {
	case LanguageTypeScript:
		return nil
	default:
		return fmt.Errorf("%w %q", ErrUnsupportedLanguage, opts.Language)
	}
}

func normalizedLanguage(opts Options) string {
	return strings.ToLower(strings.TrimSpace(opts.Language))
}

// GenerateTypeScript emits deterministic TypeScript bindings from a ModuleContract.
func GenerateTypeScript(contract shunter.ModuleContract) ([]byte, error) {
	if err := validateContract(contract); err != nil {
		return nil, err
	}
	return generateTypeScript(contract)
}

func validateContract(contract shunter.ModuleContract) error {
	if err := shunter.ValidateModuleContract(contract); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidContract, err)
	}
	for _, table := range contract.Schema.Tables {
		for _, column := range table.Columns {
			if _, err := typeScriptColumnType(column.Type); err != nil {
				return fmt.Errorf("%w: table %q column %q: %v", ErrInvalidContract, table.Name, column.Name, err)
			}
		}
	}
	return nil
}
