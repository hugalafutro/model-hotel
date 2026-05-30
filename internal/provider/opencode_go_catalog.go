package provider

var opencodeGoCatalog = loadCatalog[[]OpenCodeModelSpec]("opencode_go.json")

// GetOpenCodeGoCatalog returns the OpenCode Go model catalog.
func GetOpenCodeGoCatalog() []OpenCodeModelSpec {
	return opencodeGoCatalog
}
