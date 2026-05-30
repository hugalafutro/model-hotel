package provider

var opencodeZenCatalog = loadCatalog[[]OpenCodeModelSpec]("opencode_zen.json")

// GetOpenCodeZenCatalog returns the OpenCode Zen model catalog.
func GetOpenCodeZenCatalog() []OpenCodeModelSpec {
	return opencodeZenCatalog
}
