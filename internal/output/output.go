package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// Format selects the output rendering.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// Valid reports whether the format is supported.
func (f Format) Valid() bool {
	switch f {
	case FormatTable, FormatJSON, FormatYAML:
		return true
	}
	return false
}

// String returns the canonical format name.
func (f Format) String() string { return string(f) }

// Writer writes formatted output to stdout and reserves stderr for diagnostics.
type Writer struct {
	out    io.Writer
	err    io.Writer
	format Format
}

func NewWriter(format Format) *Writer {
	return &Writer{out: os.Stdout, err: os.Stderr, format: format}
}

// ErrWriter returns the writer reserved for diagnostics (stderr by default).
func (w *Writer) ErrWriter() io.Writer {
	return w.err
}

// Fatalf writes a diagnostic to stderr and returns an error for the caller to
// convert into a non-zero exit code.
func (w *Writer) Fatalf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(w.err, "%s\n", msg)
	return errors.New(msg)
}

// JSON marshals v and writes it to stdout with a trailing newline. No
// progress or diagnostic text is written to stdout.
func (w *Writer) JSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	_, err = fmt.Fprintln(w.out, string(data))
	return err
}

// Table renders rows of string columns with aligned columns. An empty header
// or row set produces a "No items found." message.
func (w *Writer) Table(header []string, rows [][]string) error {
	if len(rows) == 0 {
		fmt.Fprintln(w.out, "No items found.")
		return nil
	}
	wt := tabwriter.NewWriter(w.out, 0, 0, 2, ' ', 0)
	if len(header) > 0 {
		for i, h := range header {
			if i > 0 {
				fmt.Fprint(wt, "\t")
			}
			fmt.Fprint(wt, h)
		}
		fmt.Fprintln(wt)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(wt, "\t")
			}
			fmt.Fprint(wt, cell)
		}
		fmt.Fprintln(wt)
	}
	return wt.Flush()
}

// Object renders a single map as a key/value table.
func (w *Writer) Object(fields [][2]string) error {
	if len(fields) == 0 {
		fmt.Fprintln(w.out, "No items found.")
		return nil
	}
	maxKey := 0
	for _, f := range fields {
		if len(f[0]) > maxKey {
			maxKey = len(f[0])
		}
	}
	for _, f := range fields {
		fmt.Fprintf(w.out, "%s%s %s\n", f[0], strings.Repeat(" ", maxKey-len(f[0])+1), f[1])
	}
	return nil
}

// Time formats a time for table output, returning "-" for zero values.
func Time(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// OptString dereferences a *string, returning def when nil.
func OptString(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

// OptTime formats a *time.Time, returning def when nil or zero.
func OptTime(t *time.Time, def string) string {
	if t == nil || t.IsZero() {
		return def
	}
	return t.UTC().Format(time.RFC3339)
}

// SortedKeys returns the map's keys in sorted order for deterministic output.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
