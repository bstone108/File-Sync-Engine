package clioutput

import "fmt"

// ErrorLine renders the documented default CLI failure policy: concise,
// human-readable stderr lines with no structured-log prefix or timestamp.
func ErrorLine(context string, err error) string {
	if context == "" {
		return fmt.Sprintf("fse: %v", err)
	}
	return fmt.Sprintf("fse: %s: %v", context, err)
}

// FormattedLine renders usage and other formatted CLI failure text with the
// same human-readable prefix and trailing newline expected by stderr callers.
func FormattedLine(format string, args ...any) string {
	return fmt.Sprintf("fse: "+format+"\n", args...)
}
