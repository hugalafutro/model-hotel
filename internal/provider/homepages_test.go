package provider

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// officialHomepages pins every provider card's link to the address its maker
// publishes. A change here is deliberate: check the new address against the
// vendor's own channels first. koboldcpp.com is an impostor site; the project
// lives on its author's GitHub (LostRuins/koboldcpp discussion #2499).
var officialHomepages = map[string]string{
	"nanogpt":            "https://nano-gpt.com",
	"zai-coding":         "https://z.ai",
	"kimi-code":          "https://www.kimi.com",
	"minimax":            "https://www.minimax.io",
	"openai":             "https://openai.com",
	"anthropic":          "https://www.anthropic.com",
	"anthropic-messages": "https://www.anthropic.com",
	"deepseek":           "https://www.deepseek.com",
	"ollama-cloud":       "https://ollama.com",
	"ollama":             "https://github.com/ollama/ollama",
	"opencode-zen":       "https://opencode.ai",
	"opencode-go":        "https://opencode.ai",
	"xai":                "https://x.ai",
	"google":             "https://aistudio.google.com",
	"cohere":             "https://cohere.com",
	"openrouter":         "https://openrouter.ai",
	"neuralwatt":         "https://neuralwatt.com",
	"koboldcpp":          "https://github.com/LostRuins/koboldcpp",
	"lmstudio":           "https://lmstudio.ai",
	"bedrock":            "https://aws.amazon.com/bedrock",
	"azure":              "https://ai.azure.com",
	"vertex-express":     "https://cloud.google.com/vertex-ai",
}

// The dashboard's provider cards link exactly the pinned addresses, one per
// type, with nothing extra and nothing missing.
func TestDashboardHomepagesAreTheOfficialAddresses(t *testing.T) {
	cards := map[string]string{}
	entry := regexp.MustCompile(`(?m)^\s*"?([a-z0-9-]+)"?:\s*"([^"]+)"`)
	for _, m := range entry.FindAllStringSubmatch(homepagesBlock(t), -1) {
		cards[m[1]] = m[2]
	}
	for typ, want := range officialHomepages {
		if got, ok := cards[typ]; !ok {
			t.Errorf("providerHomepages has no entry for %q (want %s)", typ, want)
		} else if got != want {
			t.Errorf("providerHomepages[%q] = %s, want the official %s", typ, got, want)
		}
	}
	for typ, got := range cards {
		if _, ok := officialHomepages[typ]; !ok {
			t.Errorf("providerHomepages[%q] = %s is not pinned in officialHomepages; verify it and add it", typ, got)
		}
	}
	// Every type but the hand-entered custom one has a known home.
	for _, typ := range KnownTypes {
		if _, ok := officialHomepages[typ]; !ok && typ != "custom" {
			t.Errorf("provider type %q has no pinned official address", typ)
		}
	}
}

// README.md's provider list links the same pinned addresses. It and the cards
// were written separately and drifted: the README sent KoboldCpp readers to
// the impostor site while the card had the official repository.
func TestReadmeProviderLinksAreTheOfficialAddresses(t *testing.T) {
	const readmePath = "../../README.md"
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}
	// The paragraph runs to the next blank line, so a reflow keeps every link.
	para := regexp.MustCompile(`(?ms)^Pick a provider family.*?(?:\n\n|\z)`).Find(readme)
	if para == nil {
		t.Fatalf("provider list paragraph (\"Pick a provider family ...\") not found in %s", readmePath)
	}
	listed := map[string]bool{}
	for _, m := range regexp.MustCompile(`\]\((https?://[^)]+)\)`).FindAllSubmatch(para, -1) {
		listed[strings.TrimSuffix(string(m[1]), "/")] = true
	}
	official := map[string]bool{}
	for _, u := range officialHomepages {
		official[u] = true
	}
	for u := range listed {
		if !official[u] {
			t.Errorf("README links %s, which is not a pinned official address", u)
		}
	}
	for u := range official {
		if !listed[u] {
			t.Errorf("README's provider list does not link the official %s", u)
		}
	}
}

// homepagesBlock returns the body of the providerHomepages object literal.
func homepagesBlock(t *testing.T) string {
	t.Helper()
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
	return block
}
