// Package secretconfig reads environment or file-backed secrets without exposing
// file contents through the process environment or configuration errors.
package secretconfig

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const maxFileBytes = 16 << 10

// Value accepts NAME or NAME_FILE, never both (even if one is empty). Unset
// settings return an empty value for the caller's existing required-value checks.
// Files are bounded regular files; exactly one final LF or CRLF is removed.
func Value(lookup func(string) (string, bool), name string) (string, error) {
	value, direct := lookup(name)
	path, fromFile := lookup(name + "_FILE")
	if direct && fromFile {
		return "", fmt.Errorf("configure only one of %s and %s_FILE", name, name)
	}
	if !fromFile {
		return value, nil
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s_FILE must name a readable regular file", name)
	}
	// Check before opening so a mistaken FIFO/device cannot block startup.
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s_FILE must name a readable regular file", name)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("could not read %s_FILE", name)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s_FILE must name a readable regular file", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil || len(data) > maxFileBytes {
		return "", fmt.Errorf("%s_FILE must be readable and at most 16384 bytes", name)
	}
	value = string(data)
	if strings.HasSuffix(value, "\n") {
		value = strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r")
	}
	if value == "" || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("%s_FILE must contain a nonempty value without NUL", name)
	}
	return value, nil
}
