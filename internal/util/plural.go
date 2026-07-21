package util

import "fmt"

// Plural picks the singular or plural word for a count. It exists so
// user-facing messages read "1 model" / "2 models" instead of the
// unpluralised "1 models".
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// Count renders a count with its correctly pluralised noun, e.g.
// Count(1, "model", "models") == "1 model".
func Count(n int, singular, plural string) string {
	return fmt.Sprintf("%d %s", n, Plural(n, singular, plural))
}
