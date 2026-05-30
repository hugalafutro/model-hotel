package provider

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed catalogs/*.json
var catalogFS embed.FS

// loadCatalog reads a JSON file from the embedded catalogs/ directory
// and unmarshals it into the provided type T.
func loadCatalog[T any](name string) T {
	data, err := catalogFS.ReadFile("catalogs/" + name)
	if err != nil {
		panic(fmt.Sprintf("catalog: read %s: %v", name, err))
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		panic(fmt.Sprintf("catalog: parse %s: %v", name, err))
	}
	return result
}
