package provider

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
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

// lookupByModelID finds the catalog entry whose model ID matches id, or nil.
// key reads the ID out of an entry, since every embedded catalog carries it
// under its own field name. The result points into catalog, so a caller that
// mutates what it reads must copy first.
func lookupByModelID[T any](catalog []T, id string, key func(*T) string) *T {
	i := slices.IndexFunc(catalog, func(e T) bool { return key(&e) == id })
	if i < 0 {
		return nil
	}
	return &catalog[i]
}
