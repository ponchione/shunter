package shunter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ponchione/shunter/internal/atomicfile"
	"github.com/ponchione/shunter/schema"
)

const (
	dataDirMetadataFilename      = "shunter.datadir.json"
	dataDirMetadataFormatVersion = 1
)

var syncDataDirMetadataDir = atomicfile.SyncDir

type dataDirMetadata struct {
	FormatVersion   int                   `json:"format_version"`
	ContractVersion uint32                `json:"contract_version"`
	Shunter         dataDirShunterVersion `json:"shunter"`
	Module          dataDirModuleVersion  `json:"module"`
}

type dataDirShunterVersion struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

type dataDirModuleVersion struct {
	Name          string `json:"name"`
	Version       string `json:"version,omitempty"`
	SchemaVersion uint32 `json:"schema_version"`
}

func validateDataDirMetadata(dataDir string, mod *Module, reg schema.SchemaRegistry) error {
	metadata, ok, err := readDataDirMetadata(dataDir)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if metadata.FormatVersion != dataDirMetadataFormatVersion {
		return fmt.Errorf("data dir metadata format_version = %d, want %d", metadata.FormatVersion, dataDirMetadataFormatVersion)
	}
	if metadata.ContractVersion != ModuleContractVersion {
		return fmt.Errorf("data dir metadata contract_version = %d, want %d", metadata.ContractVersion, ModuleContractVersion)
	}
	if metadata.Module.Name != mod.name {
		return fmt.Errorf("data dir metadata module name = %q, want %q", metadata.Module.Name, mod.name)
	}
	if metadata.Module.SchemaVersion != reg.Version() {
		return fmt.Errorf("data dir metadata schema_version = %d, want %d", metadata.Module.SchemaVersion, reg.Version())
	}
	return nil
}

func readDataDirMetadata(dataDir string) (dataDirMetadata, bool, error) {
	path := filepath.Join(dataDir, dataDirMetadataFilename)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return dataDirMetadata{}, false, nil
	}
	if err != nil {
		return dataDirMetadata{}, false, fmt.Errorf("read data dir metadata %s: %w", path, err)
	}
	var metadata dataDirMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return dataDirMetadata{}, false, fmt.Errorf("parse data dir metadata %s: %w", path, err)
	}
	return metadata, true, nil
}

func writeDataDirMetadata(dataDir string, mod *Module, reg schema.SchemaRegistry) error {
	info := CurrentBuildInfo()
	metadata := dataDirMetadata{
		FormatVersion:   dataDirMetadataFormatVersion,
		ContractVersion: ModuleContractVersion,
		Shunter: dataDirShunterVersion{
			Version: info.Version,
			Commit:  info.Commit,
			Date:    info.Date,
		},
		Module: dataDirModuleVersion{
			Name:          mod.name,
			Version:       mod.version,
			SchemaVersion: reg.Version(),
		},
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data dir metadata: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(dataDir, dataDirMetadataFilename)
	if err := atomicfile.WriteFile(path, data, atomicfile.Options{
		Mode:        0o600,
		TempPattern: dataDirMetadataFilename + ".tmp-*",
		SyncDir:     syncDataDirMetadataDir,
	}); err != nil {
		return fmt.Errorf("replace data dir metadata %s: %w", path, err)
	}
	return nil
}
