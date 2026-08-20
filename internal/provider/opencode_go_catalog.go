package provider

var opencodeGoCatalog = loadCatalog[[]OpenCodeModelSpec]("opencode_go.json")

// GetOpenCodeGoCatalog returns the OpenCode Go model catalog. It is an
// override channel and legitimately empty while the live /models listing and
// models.dev (opencode-go) are correct about every model: Go usage is metered
// at models.dev's per-token rates, the shadow cost that Go's dollar-based
// quotas ($/5h, $/week, $/month) burn per request.
//
// Any future override row must state real prices: OpenCodeCatalogToModel
// materializes the price fields unconditionally, so a row added to fix, say,
// a context length would silently pin input/output price at $0 and the model
// would meter free.
func GetOpenCodeGoCatalog() []OpenCodeModelSpec {
	return opencodeGoCatalog
}
