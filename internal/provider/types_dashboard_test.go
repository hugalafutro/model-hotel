package provider

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The dashboard builds its provider-type dropdown from
// providerTypeTranslationKeys, and labels an existing provider's stored type
// through the same map. A type the backend accepts but the dashboard does not
// know is therefore invisible twice over: it cannot be picked when adding a
// provider (so the operator picks something else and loses that type's quota
// polling and native discovery), and an existing row of that type renders the
// raw i18n key as its label.
//
// This is exactly how neuralwatt broke: it had a backend host rule and quota
// support but no dropdown entry and no label in any locale.
func TestKnownTypesAreAllInTheDashboardVocabulary(t *testing.T) {
	const constantsPath = "../../web/src/pages/Providers/constants.ts"
	source, err := os.ReadFile(constantsPath)
	if err != nil {
		t.Fatalf("read %s: %v", constantsPath, err)
	}

	block := translationKeyBlock(t, string(source))
	entry := regexp.MustCompile(`(?m)^\s*"?([a-z0-9-]+)"?:\s*"([^"]+)"`)
	dashboard := map[string]string{}
	for _, m := range entry.FindAllStringSubmatch(block, -1) {
		dashboard[m[1]] = m[2]
	}
	if len(dashboard) == 0 {
		t.Fatal("parsed no entries out of providerTypeTranslationKeys; the guard would pass vacuously")
	}

	const localePath = "../../web/src/i18n/locales/en.json"
	raw, err := os.ReadFile(localePath)
	if err != nil {
		t.Fatalf("read %s: %v", localePath, err)
	}
	var locale struct {
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(raw, &locale); err != nil {
		t.Fatalf("parse %s: %v", localePath, err)
	}

	for _, typ := range KnownTypes {
		key, ok := dashboard[typ]
		if !ok {
			t.Errorf("provider type %q has no entry in providerTypeTranslationKeys (%s): it cannot be picked in the add dialog", typ, constantsPath)
			continue
		}
		name := strings.TrimPrefix(key, "providers.")
		if _, ok := locale.Providers[name]; !ok {
			t.Errorf("provider type %q maps to %q, which is missing from %s: the dashboard would render the raw key", typ, key, localePath)
		}
	}
}

// translationKeyBlock returns the body of the providerTypeTranslationKeys
// object literal.
func translationKeyBlock(t *testing.T, source string) string {
	t.Helper()
	const marker = "providerTypeTranslationKeys: Record<string, string> = {"
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatalf("providerTypeTranslationKeys not found; this guard needs updating alongside the rename")
	}
	rest := source[start+len(marker):]
	end := strings.Index(rest, "\n};")
	if end < 0 {
		t.Fatal("providerTypeTranslationKeys is not terminated as expected")
	}
	return rest[:end]
}
