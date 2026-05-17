package read

import (
	"runtime"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// normalizeMacOSPath applies macOS-specific path normalization: replaces curly
// quotes with straight quotes, replaces Unicode spaces with regular ASCII space,
// and applies NFD Unicode normalization on macOS (matching macOS filesystem
// behavior).
func normalizeMacOSPath(path string) string {
	path = replaceCurlyQuotes(path)
	path = replaceUnicodeSpaces(path)

	if runtime.GOOS == "darwin" {
		path = norm.NFD.String(path)
	}

	return path
}

func replaceCurlyQuotes(s string) string {
	replacer := strings.NewReplacer(
		"“", "\"", // left double quotation mark
		"”", "\"", // right double quotation mark
		"‘", "'", // left single quotation mark
		"’", "'", // right single quotation mark
	)

	return replacer.Replace(s)
}

func replaceUnicodeSpaces(s string) string {
	replacer := strings.NewReplacer(
		" ", " ", // non-breaking space
		" ", " ", // en space
		" ", " ", // em space
		" ", " ", // figure space
		" ", " ", // thin space
		" ", " ", // narrow no-break space
	)

	return replacer.Replace(s)
}
