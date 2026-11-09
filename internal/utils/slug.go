package utils

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// GenerateSlug creates unique (hopefully) slugs for dish descriptions
func GenerateSlug(input string) string {
	// to prevent repetitions
	timestamp := time.Now().UTC().Format("20060102150405")
	defaultSlug := fmt.Sprintf("default-slug-%s", timestamp)
	if input == "" {
		return defaultSlug
	}

	// 1. Normalize Unicode (decompose combined characters, e.g., é -> e + ´)
	t := transform.Chain(
		norm.NFD,                           // Normalize to decomposed form
		runes.Remove(runes.In(unicode.Mn)), // Remove combining marks (accents)
		norm.NFC,                           // Recompose for consistency
	)

	normalized, _, _ := transform.String(t, input)

	// 2. Convert to lowercase
	normalized = strings.ToLower(normalized)

	// 3. Replace spaces and punctuation with hyphens
	// Allow letters, numbers, and hyphens only
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := re.ReplaceAllString(normalized, "-")

	// 4. Trim hyphens from start and end
	slug = strings.Trim(slug, "-")

	// 5. Collapse multiple hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}

	// 6. Enforce reasonable length (optional, but safe)
	if len(slug) > 200 {
		slug = slug[:200]
		slug = strings.TrimSuffix(slug, "-")
	}

	// Handle edge cases
	if slug == "" {
		return defaultSlug
	}

	return slug
}
