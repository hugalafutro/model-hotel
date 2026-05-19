package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/hugalafutro/model-hotel/internal/config"
)

const asciiBanner = `███╗   ███╗ ██████╗ ██████╗ ███████╗██╗         ██╗  ██╗ ██████╗ ████████╗███████╗██╗
████╗ ████║██╔═══██╗██╔══██╗██╔════╝██║         ██║  ██║██╔═══██╗╚══██╔══╝██╔════╝██║
██╔████╔██║██║   ██║██║  ██║█████╗  ██║         ███████║██║   ██║   ██║   █████╗  ██║
██║╚██╔╝██║██║   ██║██║  ██║██╔══╝  ██║         ██╔══██║██║   ██║   ██║   ██╔══╝  ██║
██║ ╚═╝ ██║╚██████╔╝██████╔╝███████╗███████╗    ██║  ██║╚██████╔╝   ██║   ███████╗███████╗
╚═╝     ╚═╝ ╚═════╝ ╚═════╝ ╚══════╝╚══════╝    ╚═╝  ╚═╝ ╚═════╝    ╚═╝   ╚══════╝╚══════╝`

// printStartupBanner prints the ASCII art logo and config summary box.
// Uses direct stdout (not slog) because slog escapes \n, making ASCII art
// unreadable. The entire banner is built as a single string and written
// in one call to minimize the window where Docker Compose can interleave
// output from other containers between lines.
// Printed after all other startup output so Docker log interleaving
// from other containers (e.g. postgres) cannot split the banner.
func printStartupBanner(w io.Writer, cfg *config.Config) {
	var b strings.Builder
	b.WriteByte('\n')
	b.WriteString(asciiBanner)
	b.WriteByte('\n')
	b.WriteString(cfg.String())
	_, _ = fmt.Fprint(w, b.String())
}

// printAdminTokenBox prints the one-time admin token box shown only on
// first launch when the token was auto-generated. Placed right after the
// config box so it stays grouped with the banner output.
func printAdminTokenBox(w io.Writer, token string) {
	// Base box is 64 runes wide (║ + 62 content chars + ║).
	// The token line format is "║  TOKEN PADDING║" where "║  " is 3 runes
	// and "║" is 1 rune, leaving 60 runes for token + padding.
	const baseContentWidth = 60

	// If the token exceeds the base width, widen the box to fit.
	contentWidth := baseContentWidth
	tokenWidth := utf8.RuneCountInString(token)
	if tokenWidth > contentWidth {
		contentWidth = tokenWidth + 1 // +1 for minimum trailing space
	}

	padding := contentWidth - tokenWidth
	extra := contentWidth - baseContentWidth
	extraPad := strings.Repeat("═", extra)
	extraSpace := strings.Repeat(" ", extra)

	// Build the entire box as one string and write in a single call
	// to minimize Docker log interleaving.
	var b strings.Builder
	fmt.Fprintf(&b, `
╔══════════════════════════════════════════════════════════════%s╗
║  ADMIN TOKEN (save now — this will NOT be shown again):      %s║
║                                                              %s║
║  %s%s║
║                                                              %s║
║  To regenerate: delete the admin-token file and restart.     %s║
╚══════════════════════════════════════════════════════════════%s╝
`, extraPad, extraSpace, extraSpace, token, strings.Repeat(" ", padding), extraSpace, extraSpace, extraPad)
	_, _ = fmt.Fprint(w, b.String())
}

// printReadyMessage prints the final "instance is up and running" line.
// Called after the database is connected and all startup tasks are done,
// so it serves as the definitive confirmation that the server is ready.
func printReadyMessage(w io.Writer, version string) {
	_, _ = fmt.Fprintf(w, "Model Hotel (%s) instance is up and running.\n", version)
}

// printStartupBannerStdout, printAdminTokenBoxStdout, and
// printReadyMessageStdout are thin wrappers that write to os.Stdout.
// They exist so main() stays readable while the core functions accept
// an io.Writer for testability.

func printStartupBannerStdout(cfg *config.Config) {
	printStartupBanner(os.Stdout, cfg)
}

func printAdminTokenBoxStdout(token string) {
	printAdminTokenBox(os.Stdout, token)
}

func printReadyMessageStdout(version string) {
	printReadyMessage(os.Stdout, version)
}
