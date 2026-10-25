package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatValid(t *testing.T) {
	for _, f := range []Format{FormatTable, FormatJSON, FormatYAML} {
		if !f.Valid() {
			t.Errorf("Format(%q).Valid() = false, want true", f)
		}
	}
	if Format("xml").Valid() {
		t.Error("Format(xml).Valid() = true, want false")
	}
}

func TestTime(t *testing.T) {
	if got := Time(time.Time{}); got != "-" {
		t.Errorf("Time(zero) = %q, want %q", got, "-")
	}
	tm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := Time(tm)
	if !strings.HasPrefix(got, "2026-01-02T03:04:05Z") {
		t.Errorf("Time(t) = %q, want prefix 2026-01-02T03:04:05Z", got)
	}
}

func TestOptString(t *testing.T) {
	if got := OptString(nil, "default"); got != "default" {
		t.Errorf("OptString(nil) = %q, want %q", got, "default")
	}
	s := "value"
	if got := OptString(&s, "default"); got != "value" {
		t.Errorf("OptString(&value) = %q, want %q", got, "value")
	}
}

func TestOptTime(t *testing.T) {
	if got := OptTime(nil, "default"); got != "default" {
		t.Errorf("OptTime(nil) = %q, want %q", got, "default")
	}
	tm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := OptTime(&tm, "default"); !strings.HasPrefix(got, "2026-01-02") {
		t.Errorf("OptTime(&t) = %q, want prefix 2026-01-02", got)
	}
	if got := OptTime(&time.Time{}, "default"); got != "default" {
		t.Errorf("OptTime(zero) = %q, want %q", got, "default")
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]int{"z": 1, "a": 2, "m": 3}
	keys := SortedKeys(m)
	want := []string{"a", "m", "z"}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("SortedKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(FormatTable)
	w.out = &buf
	if err := w.Table([]string{"A", "B"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No items found.") {
		t.Errorf("expected 'No items found.' in output, got %q", buf.String())
	}
}

func TestTableRows(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(FormatTable)
	w.out = &buf
	rows := [][]string{{"a", "1"}, {"b", "2"}}
	if err := w.Table([]string{"Key", "Value"}, rows); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "a") || !strings.Contains(out, "1") {
		t.Errorf("expected table to contain row data, got %q", out)
	}
}

func TestObject(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(FormatTable)
	w.out = &buf
	fields := [][2]string{{"Name", "test"}, {"Phase", "Running"}}
	if err := w.Object(fields); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "test") || !strings.Contains(out, "Running") {
		t.Errorf("expected object output to contain fields, got %q", out)
	}
}
