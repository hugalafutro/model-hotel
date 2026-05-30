package provider

var xaiCatalog = loadCatalog[[]OpenCodeModelSpec]("xai.json")

// GetXAICatalog returns the XAI model catalog.
func GetXAICatalog() []OpenCodeModelSpec {
	return xaiCatalog
}
