package encoding

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// DecodeAppleScriptOutput intelligently decodes AppleScript output. It first trims whitespace,
// then checks for valid UTF-8, and if that fails, it falls back to decoding from MacRoman.
// The final string is sanitized to ensure it's valid UTF-8.
func DecodeAppleScriptOutput(input []byte) (string, error) {
	// Trim trailing newlines and other whitespace from osascript output. This is
	// the key step to prevent intermittent decoding errors.
	trimmedInput := bytes.TrimSpace(input)

	var decoded string
	var err error

	if utf8.Valid(trimmedInput) {
		decoded = string(trimmedInput)
	} else {
		// If the input is not valid UTF-8, assume it's MacRoman.
		reader := charmap.Macintosh.NewDecoder().Reader(bytes.NewReader(trimmedInput))
		var output []byte
		output, err = io.ReadAll(reader)
		if err != nil {
			return "", err
		}
		decoded = string(output)
	}

	// Sanitize the string to remove any invalid UTF-8 characters as a final safety measure.
	return strings.ToValidUTF8(decoded, ""), nil
}