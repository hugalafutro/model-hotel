package provider

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// README.md's provider list and the dashboard's providerHomepages map both
// link each provider's home page. They were written separately and drifted:
// the README sent KoboldCpp readers to an impostor site while the card had
// the official repository. This keeps the two to one set of URLs.
func TestReadmeProviderLinksMatchDashboardHomepages(t *testing.T) {
	const constantsPath = "../../web/src/pages/Providers/constants.ts"
	source, err := os.ReadFile(constantsPath)
	if err != nil {
		t.Fatalf("read %s: %v", constantsPath, err)
	}
	const marker = "providerHomepages: Record<string, string> = {"
	_, after, found := strings.Cut(string(source), marker)
	if !found {
		t.Fatalf("providerHomepages not found in %s; this guard needs updating alongside the rename", constantsPath)
	}
	block, _, ok := strings.Cut(after, "\n};")
	if !ok {
		t.Fatal("providerHomepages is not terminated as expected")
	}
	cards := urlSet(regexp.MustCompile(`:\s*"(https?://[^"]+)"`).FindAllStringSubmatch(block, -1))

	const readmePath = "../../README.md"
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}
	line := regexp.MustCompile(`(?m)^Pick a provider family.*$`).Find(readme)
	if line == nil {
		t.Fatalf("provider list paragraph (\"Pick a provider family ...\") not found in %s", readmePath)
	}
	listed := urlSet(regexp.MustCompile(`\]\((https?://[^)]+)\)`).FindAllSubmatch(line, -1))

	if len(cards) == 0 || len(listed) == 0 {
		t.Fatalf("parsed %d card URLs and %d README URLs; the guard would pass vacuously", len(cards), len(listed))
	}
	for u := range listed {
		if !cards[u] {
			t.Errorf("README links %s, which is not in providerHomepages (%s)", u, constantsPath)
		}
	}
	for u := range cards {
		if !listed[u] {
			t.Errorf("providerHomepages has %s, which README's provider list does not link", u)
		}
	}
}

// urlSet collects the first capture group of each match, ignoring a trailing
// slash so "https://x.ai/" and "https://x.ai" count as the same link.
func urlSet[S string | []byte](matches [][]S) map[string]bool {
	set := map[string]bool{}
	for _, m := range matches {
		set[strings.TrimSuffix(string(m[1]), "/")] = true
	}
	return set
}
