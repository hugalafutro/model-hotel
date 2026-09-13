package provider

var opencodeZenCatalog = loadCatalog[[]OpenCodeModelSpec]("opencode_zen.json")

// GetOpenCodeZenCatalog returns the OpenCode Zen model catalog. It is a
// metadata override channel: a row exists only to state what neither the live
// listing nor models.dev gets right, today the input modalities of two free
// models whose deployment rejects the audio or video input models.dev
// advertises for them. Rows carry no price (models.dev's opencode entry prices
// every Zen model, free ones at zero) and never surface a model: discovery
// backfills live models from the catalog and unions nothing in, so a model Zen
// drops from its listing is gone with it.
func GetOpenCodeZenCatalog() []OpenCodeModelSpec {
	return opencodeZenCatalog
}
