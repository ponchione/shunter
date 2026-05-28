package schema

import (
	"errors"
	"fmt"
)

// TableSDKVisibility describes how generated SDK profiles should treat a table.
type TableSDKVisibility string

const (
	TableSDKVisibilityPublic   TableSDKVisibility = "public"
	TableSDKVisibilityInternal TableSDKVisibility = "internal"
	TableSDKVisibilityPrivate  TableSDKVisibility = "private"
	TableSDKVisibilitySystem   TableSDKVisibility = "system"
)

// ErrInvalidTableSDKMetadata reports malformed table SDK metadata.
var ErrInvalidTableSDKMetadata = errors.New("invalid table SDK metadata")

// TableSDKMetadata records passive SDK generation metadata for one table.
type TableSDKMetadata struct {
	Visibility TableSDKVisibility `json:"visibility"`
}

// ValidateTableSDKMetadata verifies that table SDK metadata uses a supported
// visibility value.
func ValidateTableSDKMetadata(metadata TableSDKMetadata) error {
	switch metadata.Visibility {
	case TableSDKVisibilityPublic,
		TableSDKVisibilityInternal,
		TableSDKVisibilityPrivate,
		TableSDKVisibilitySystem:
		return nil
	default:
		return fmt.Errorf("%w: visibility %q", ErrInvalidTableSDKMetadata, metadata.Visibility)
	}
}

func defaultTableSDKMetadata() TableSDKMetadata {
	return TableSDKMetadata{Visibility: TableSDKVisibilityPublic}
}

func tableSDKMetadataOrDefault(metadata TableSDKMetadata) TableSDKMetadata {
	if metadata.Visibility == "" {
		return defaultTableSDKMetadata()
	}
	return metadata
}

func systemTableSDKMetadata() TableSDKMetadata {
	return TableSDKMetadata{Visibility: TableSDKVisibilitySystem}
}
