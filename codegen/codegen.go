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

	// DefaultTypeScriptRuntimeImport is the stable local package name imported by generated bindings.
	DefaultTypeScriptRuntimeImport = "@shunter/client"

	// ProfileDefault preserves the current generator profile.
	ProfileDefault = ""
	// ProfileInternal emits the complete internal TypeScript surface.
	ProfileInternal = "internal"
	// ProfileFull emits the complete TypeScript surface.
	ProfileFull = "full"
	// ProfilePublic selects the public TypeScript surface.
	ProfilePublic = "public"
)

var (
	// ErrUnsupportedLanguage reports a requested generator target that does not exist.
	ErrUnsupportedLanguage = errors.New("unsupported language")

	// ErrUnsupportedProfile reports a requested generator profile that does not exist.
	ErrUnsupportedProfile = errors.New("unsupported codegen profile")

	// ErrInvalidContract reports canonical contract input that cannot be used for codegen.
	ErrInvalidContract = errors.New("invalid module contract")
)

// Options configures client binding generation.
type Options struct {
	Language                string
	TypeScriptRuntimeImport string
	Profile                 string
}

// TypeScriptOptions configures generated TypeScript bindings.
type TypeScriptOptions struct {
	RuntimeImport string
	Profile       string
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
		return GenerateTypeScriptWithOptions(contract, TypeScriptOptions{
			RuntimeImport: opts.TypeScriptRuntimeImport,
			Profile:       opts.Profile,
		})
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedLanguage, opts.Language)
	}
}

// ValidateOptions rejects unsupported generator options before input decoding or file I/O.
func ValidateOptions(opts Options) error {
	switch normalizedLanguage(opts) {
	case LanguageTypeScript:
		if _, err := normalizedTypeScriptRuntimeImport(opts.TypeScriptRuntimeImport); err != nil {
			return err
		}
		_, err := normalizedProfile(opts.Profile)
		return err
	default:
		return fmt.Errorf("%w %q", ErrUnsupportedLanguage, opts.Language)
	}
}

func normalizedLanguage(opts Options) string {
	return strings.ToLower(strings.TrimSpace(opts.Language))
}

// GenerateTypeScript emits deterministic TypeScript bindings from a ModuleContract.
func GenerateTypeScript(contract shunter.ModuleContract) ([]byte, error) {
	return GenerateTypeScriptWithOptions(contract, TypeScriptOptions{})
}

// GenerateTypeScriptWithOptions emits deterministic TypeScript bindings from a ModuleContract.
func GenerateTypeScriptWithOptions(contract shunter.ModuleContract, opts TypeScriptOptions) ([]byte, error) {
	if err := validateContract(contract); err != nil {
		return nil, err
	}
	runtimeImport, err := normalizedTypeScriptRuntimeImport(opts.RuntimeImport)
	if err != nil {
		return nil, err
	}
	profile, err := normalizedProfile(opts.Profile)
	if err != nil {
		return nil, err
	}
	return generateTypeScript(contract, typeScriptGenerationOptions{
		runtimeImport: runtimeImport,
		profile:       profile,
	})
}

func normalizedTypeScriptRuntimeImport(specifier string) (string, error) {
	trimmed := strings.TrimSpace(specifier)
	if trimmed == "" {
		return DefaultTypeScriptRuntimeImport, nil
	}
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("invalid TypeScript runtime import specifier %q: control characters are not allowed", specifier)
		}
	}
	return trimmed, nil
}

func normalizedProfile(profile string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(profile))
	switch trimmed {
	case ProfileDefault:
		return ProfileInternal, nil
	case ProfileInternal, ProfileFull, ProfilePublic:
		return trimmed, nil
	default:
		return "", fmt.Errorf("%w %q (supported profiles: %s, %s, %s)", ErrUnsupportedProfile, profile, ProfileInternal, ProfileFull, ProfilePublic)
	}
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
