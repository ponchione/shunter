package shunter

type moduleSnapshot struct {
	name              string
	version           string
	metadata          map[string]string
	reducers          []ReducerDeclaration
	procedures        []ProcedureDeclaration
	queries           []queryDeclaration
	views             []viewDeclaration
	visibilityFilters []VisibilityFilterDescription
	migration         MigrationMetadata
	tableMigrations   map[string]MigrationMetadata
	migrationHooks    []MigrationHook
}

func newModuleSnapshot(mod *Module, visibilityFilters []VisibilityFilterDescription) moduleSnapshot {
	if mod == nil {
		return moduleSnapshot{metadata: map[string]string{}, tableMigrations: map[string]MigrationMetadata{}}
	}
	return moduleSnapshot{
		name:              mod.name,
		version:           mod.version,
		metadata:          mod.MetadataMap(),
		reducers:          copyReducerDeclarations(mod.reducers),
		procedures:        copyProcedureDeclarations(mod.procedures),
		queries:           copyQueryDeclarations(mod.queries),
		views:             copyViewDeclarations(mod.views),
		visibilityFilters: copyVisibilityFilterDescriptions(visibilityFilters),
		migration:         copyMigrationMetadata(mod.migration),
		tableMigrations:   copyMigrationMetadataMap(mod.tableMigrations),
		migrationHooks:    copySlice(mod.migrationHooks),
	}
}

func (s moduleSnapshot) describe() ModuleDescription {
	return ModuleDescription{
		Name:              s.name,
		Version:           s.version,
		Metadata:          copyStringMap(s.metadata),
		Queries:           describeQueryDeclarations(s.queries),
		Procedures:        describeProcedureDeclarations(s.procedures),
		Views:             describeViewDeclarations(s.views),
		VisibilityFilters: copyVisibilityFilterDescriptions(s.visibilityFilters),
		Migration:         copyMigrationMetadata(s.migration),
		TableMigrations:   copyMigrationMetadataMap(s.tableMigrations),
	}
}

func (s moduleSnapshot) reducerDeclarations() []ReducerDeclaration {
	return copyReducerDeclarations(s.reducers)
}

func (s moduleSnapshot) lookupProcedure(name string) (ProcedureDeclaration, bool) {
	for _, procedure := range s.procedures {
		if procedure.Name == name {
			return copyProcedureDeclaration(procedure), true
		}
	}
	return ProcedureDeclaration{}, false
}
