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
// Uses fmt.Println directly (not slog) because slog escapes \n, making
// ASCII art unreadable. Printed before DB connection so it appears at the
// top of the log, before any DB noise from other containers.
func printStartupBanner(w io.Writer, cfg *config.Config) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, asciiBanner)
	_, _ = fmt.Fprintln(w, cfg)
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

	_, _ = fmt.Fprintf(w, `
╔══════════════════════════════════════════════════════════════%s╗
║  ADMIN TOKEN (save now — this will NOT be shown again):      %s║
║                                                              %s║
║  %s%s║
║                                                              %s║
║  To regenerate: delete the admin-token file and restart.     %s║
╚══════════════════════════════════════════════════════════════%s╝
`, extraPad, extraSpace, extraSpace, token, strings.Repeat(" ", padding), extraSpace, extraSpace, extraPad)
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
