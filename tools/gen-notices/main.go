// Command gen-notices regenerates THIRD-PARTY-NOTICES.md at the repo root.
//
// It collects the license files of everything actually distributed in a
// release: the Go modules compiled into ./cmd/server and the production
// (bundled) frontend npm packages. Dev-only tooling (test runners, linters,
// build plugins) is intentionally excluded — it is not shipped, so it carries
// no attribution obligation.
//
// License texts are reproduced verbatim and deduplicated by content, so packages
// that share an identical license file are listed together under one copy. This
// preserves every upstream copyright notice (the core requirement of the MIT,
// BSD and Apache licenses) without bloating the file with hundreds of identical
// copies.
//
// Run via `make notices` (or `go run ./tools/gen-notices` from the repo root).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// output runs cmd and returns its stdout, wrapping any failure with the
// command's stderr so a broken `make notices` is actionable.
func output(cmd *exec.Cmd) ([]byte, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", strings.Join(cmd.Args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// dep is one distributed third-party component.
type dep struct {
	Name     string // module path or npm package name
	Version  string
	Ecosys   string // "Go" or "npm"
	License  string // SPDX-ish identifier (best effort)
	Homepage string
	Text     string // verbatim license file text ("" if none found)
}

func main() {
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}

	goDeps, err := collectGo(root)
	if err != nil {
		fatal(fmt.Errorf("collecting Go modules: %w", err))
	}
	npmDeps, err := collectNPM(root)
	if err != nil {
		fatal(fmt.Errorf("collecting npm packages: %w", err))
	}

	out := render(goDeps, npmDeps)
	dest := filepath.Join(root, "THIRD-PARTY-NOTICES.md")
	if err := os.WriteFile(dest, []byte(out), 0o644); err != nil { //nolint:gosec // generated notices file is committed and meant to be world-readable
		fatal(err)
	}
	fmt.Printf("wrote %s (%d Go modules, %d npm packages)\n", dest, len(goDeps), len(npmDeps))
}

// repoRoot returns the git top-level directory.
func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// collectGo lists the modules compiled into the server binary and reads each
// module's license file from the module cache.
func collectGo(root string) ([]dep, error) {
	cmd := exec.Command("go", "list", "-deps", "-json", "./cmd/server/")
	cmd.Dir = root
	out, err := output(cmd)
	if err != nil {
		return nil, err
	}

	type goMod struct {
		Path    string
		Version string
		Dir     string
		Main    bool
	}
	type goPkg struct {
		Module *goMod
	}

	seen := map[string]dep{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p goPkg
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		m := p.Module
		if m == nil || m.Main || m.Dir == "" {
			continue // stdlib (no module) or our own module
		}
		if _, ok := seen[m.Path]; ok {
			continue
		}
		text := readLicenseFile(m.Dir)
		seen[m.Path] = dep{
			Name:     m.Path,
			Version:  m.Version,
			Ecosys:   "Go",
			License:  detectLicense(text),
			Homepage: "https://" + m.Path,
			Text:     text,
		}
	}

	return sortedDeps(seen), nil
}

// pnpmPkg mirrors the relevant fields of `pnpm licenses list --json` entries.
type pnpmPkg struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
	Paths    []string `json:"paths"`
	License  string   `json:"license"`
	Homepage string   `json:"homepage"`
}

// collectNPM lists the production (bundled) frontend packages and reads each
// package's license file from the pnpm store.
func collectNPM(root string) ([]dep, error) {
	cmd := exec.Command("pnpm", "licenses", "list", "--prod", "--json")
	cmd.Dir = filepath.Join(root, "web")
	out, err := output(cmd)
	if err != nil {
		return nil, err
	}

	byLicense := map[string][]pnpmPkg{}
	if err := json.Unmarshal(out, &byLicense); err != nil {
		return nil, err
	}

	seen := map[string]dep{}
	for spdx, pkgs := range byLicense {
		for _, p := range pkgs {
			// pnpm reports parallel Versions/Paths arrays — one entry per
			// installed instance. Emit each so a package present at two
			// versions is fully attributed (and paired with its own path),
			// rather than collapsed onto a single version/path.
			n := len(p.Versions)
			if len(p.Paths) > n {
				n = len(p.Paths)
			}
			for i := 0; i < n; i++ {
				version := ""
				if i < len(p.Versions) {
					version = p.Versions[i]
				}
				text := ""
				if i < len(p.Paths) {
					text = readLicenseFile(p.Paths[i])
				}
				key := p.Name + "@" + version
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = dep{
					Name:     p.Name,
					Version:  version,
					Ecosys:   "npm",
					License:  spdx,
					Homepage: p.Homepage,
					Text:     text,
				}
			}
		}
	}

	return sortedDeps(seen), nil
}

func sortedDeps(m map[string]dep) []dep {
	deps := make([]dep, 0, len(m))
	for _, d := range m {
		deps = append(deps, d)
	}
	sort.Slice(deps, func(i, j int) bool {
		if ni, nj := strings.ToLower(deps[i].Name), strings.ToLower(deps[j].Name); ni != nj {
			return ni < nj
		}
		return deps[i].Version < deps[j].Version
	})
	return deps
}

// isLicenseFile reports whether an uppercased filename looks like a license or
// copying file (NOTICE is handled separately).
func isLicenseFile(upper string) bool {
	return strings.HasPrefix(upper, "LICENSE") ||
		strings.HasPrefix(upper, "LICENCE") ||
		strings.HasPrefix(upper, "COPYING") ||
		upper == "UNLICENSE"
}

// readLicenseFile returns the combined text of every license/copying file in
// dir (e.g. a dual-licensed package shipping both LICENSE-MIT and
// LICENSE-APACHE), deduplicated and in deterministic filename order, with any
// NOTICE file appended (required by Apache-2.0). Returns "" when none is found.
func readLicenseFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // deterministic across filesystems

	var texts []string
	seen := map[string]bool{} // dedup identical files (e.g. LICENSE == LICENSE.md)
	var notice string
	for _, name := range names {
		upper := strings.ToUpper(name)
		isNotice := upper == "NOTICE" || strings.HasPrefix(upper, "NOTICE.")
		if !isLicenseFile(upper) && !isNotice {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // reads license files from resolved dependency directories
		if err != nil {
			continue
		}
		body := strings.TrimSpace(string(b))
		if body == "" {
			continue
		}
		if isNotice {
			notice = body
			continue
		}
		sum := sha256.Sum256([]byte(body))
		key := hex.EncodeToString(sum[:])
		if seen[key] {
			continue
		}
		seen[key] = true
		texts = append(texts, body)
	}

	text := strings.Join(texts, "\n\n----- ----- -----\n\n")
	if notice != "" {
		if text != "" {
			text += "\n\n----- NOTICE -----\n\n"
		}
		text += notice
	}
	return text
}

// detectLicense makes a best-effort SPDX guess from license text. Used for Go
// modules, which (unlike npm) do not declare a machine-readable license.
func detectLicense(text string) string {
	t := strings.ToLower(text)
	switch {
	case text == "":
		return "UNKNOWN"
	case strings.Contains(t, "apache license") && strings.Contains(t, "version 2.0"):
		return "Apache-2.0"
	case strings.Contains(t, "mozilla public license") && strings.Contains(t, "2.0"):
		return "MPL-2.0"
	case strings.Contains(t, "redistribution and use") && strings.Contains(t, "neither the name"):
		return "BSD-3-Clause"
	case strings.Contains(t, "redistribution and use"):
		return "BSD-2-Clause"
	case strings.Contains(t, "permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(t, "permission to use, copy, modify"):
		return "ISC"
	case strings.Contains(t, "sil open font license"):
		return "OFL-1.1"
	default:
		return "see text"
	}
}

var copyrightLine = regexp.MustCompile(`(?im)^\s*(Copyright|\(c\)|©).*$`)

// firstCopyright extracts the first copyright line from license text, for the
// summary table.
func firstCopyright(text string) string {
	if m := copyrightLine.FindString(text); m != "" {
		return strings.TrimSpace(m)
	}
	return ""
}

func render(goDeps, npmDeps []dep) string {
	var b strings.Builder
	all := append(append([]dep{}, goDeps...), npmDeps...)

	b.WriteString("# Third-Party Notices\n\n")
	b.WriteString("<!-- DO NOT EDIT BY HAND. Regenerate with `make notices`. -->\n\n")
	b.WriteString("Model Hotel is distributed under the MIT License (see [LICENSE](./LICENSE)).\n")
	b.WriteString("It bundles the third-party open-source components listed below; each is the\n")
	b.WriteString("property of its respective authors and is used under the terms reproduced here.\n\n")
	fmt.Fprintf(&b, "_%d Go modules, %d npm packages (regenerate with `make notices`)._\n\n",
		len(goDeps), len(npmDeps))

	b.WriteString("## Fonts\n\n")
	b.WriteString("The web UI embeds the JetBrains Mono, Onest, and Schibsted Grotesk typefaces, ")
	b.WriteString("and KaTeX ships its own math fonts. All are licensed under the ")
	b.WriteString("**SIL Open Font License 1.1**, which permits embedding and redistribution; ")
	b.WriteString("their license files are included with the font packages below. No on-screen ")
	b.WriteString("attribution is required.\n\n")

	b.WriteString("## Summary\n\n")
	b.WriteString("| Component | Version | Ecosystem | License |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, d := range all {
		fmt.Fprintf(&b, "| [%s](%s) | %s | %s | %s |\n",
			d.Name, d.Homepage, d.Version, d.Ecosys, d.License)
	}
	b.WriteString("\n## License texts\n\n")
	b.WriteString("Packages that ship an identical license file are grouped under a single copy.\n\n")

	writeGroupedTexts(&b, all)
	return b.String()
}

// writeGroupedTexts deduplicates license texts by content and emits each unique
// text once, listing all components that share it.
func writeGroupedTexts(b *strings.Builder, deps []dep) {
	type group struct {
		text    string
		members []dep
	}
	order := []string{}
	groups := map[string]*group{}
	var missing []dep

	for _, d := range deps {
		if strings.TrimSpace(d.Text) == "" {
			missing = append(missing, d)
			continue
		}
		sum := sha256.Sum256([]byte(d.Text))
		key := hex.EncodeToString(sum[:])
		g, ok := groups[key]
		if !ok {
			g = &group{text: d.Text}
			groups[key] = g
			order = append(order, key)
		}
		g.members = append(g.members, d)
	}

	// Largest groups first, so shared licenses (MIT, Apache) lead.
	sort.SliceStable(order, func(i, j int) bool {
		return len(groups[order[i]].members) > len(groups[order[j]].members)
	})

	for _, key := range order {
		g := groups[key]
		sort.Slice(g.members, func(i, j int) bool {
			return strings.ToLower(g.members[i].Name) < strings.ToLower(g.members[j].Name)
		})
		names := make([]string, 0, len(g.members))
		for _, m := range g.members {
			names = append(names, fmt.Sprintf("`%s@%s`", m.Name, m.Version))
		}
		fmt.Fprintf(b, "### %s\n\n", g.members[0].License)
		if cr := firstCopyright(g.text); cr != "" {
			fmt.Fprintf(b, "%s\n\n", cr)
		}
		fmt.Fprintf(b, "Applies to: %s\n\n", strings.Join(names, ", "))
		b.WriteString("```\n")
		b.WriteString(strings.TrimRight(g.text, "\n"))
		b.WriteString("\n```\n\n")
	}

	if len(missing) > 0 {
		b.WriteString("### Components without a bundled license file\n\n")
		b.WriteString("The following declare a license in metadata but ship no license file in ")
		b.WriteString("the package; consult the upstream repository for the full text.\n\n")
		for _, d := range missing {
			fmt.Fprintf(b, "- `%s@%s` — %s ([%s](%s))\n", d.Name, d.Version, d.License, d.Homepage, d.Homepage)
		}
		b.WriteString("\n")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen-notices:", err)
	os.Exit(1)
}
