package cli

import (
	"testing"
)

func TestRuntimeFromExtension(t *testing.T) {
	tests := []struct {
		path   string
		want   string
		wantOK bool
	}{
		{"main.py", "python", true},
		{"script.PY", "python", true},
		{"main.go", "go", true},
		{"Main.JAVA", "java", true},
		{"app.js", "javascript", true},
		{"app.mjs", "javascript", true},
		{"app.ts", "typescript", true},
		{"main.c", "c", true},
		{"main.cpp", "cpp", true},
		{"main.cc", "cpp", true},
		{"main.cxx", "cpp", true},
		{"main.rs", "rust", true},
		{"Program.cs", "csharp", true},
		{"script.sh", "shell", true},
		{"Dockerfile", "", false},
		{"Makefile", "", false},
		{"noextension", "", false},
		{".hidden", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := runtimeFromExtension(tt.path)
			if ok != tt.wantOK {
				t.Errorf("runtimeFromExtension(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("runtimeFromExtension(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestNormalizeRuntime(t *testing.T) {
	tests := []struct {
		in  string
		wan string
	}{
		{"python", "python"},
		{"python3", "python"},
		{"go", "go"},
		{"golang", "go"},
		{"java", "java"},
		{"js", "javascript"},
		{"node", "javascript"},
		{"nodejs", "javascript"},
		{"javascript", "javascript"},
		{"ts", "typescript"},
		{"typescript", "typescript"},
		{"c", "c"},
		{"cpp", "cpp"},
		{"c++", "cpp"},
		{"rust", "rust"},
		{"rs", "rust"},
		{"cs", "csharp"},
		{"csharp", "csharp"},
		{"dotnet", "csharp"},
		{"sh", "shell"},
		{"bash", "shell"},
		{"shell", "shell"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normalizeRuntime(tt.in); got != tt.wan {
				t.Errorf("normalizeRuntime(%q) = %q, want %q", tt.in, got, tt.wan)
			}
		})
	}
}

func TestTerminalPhase(t *testing.T) {
	for _, p := range []string{"Succeeded", "Failed", "TimedOut", "Cancelled"} {
		if !terminalPhase(p) {
			t.Errorf("terminalPhase(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"Pending", "Running", "Starting", ""} {
		if terminalPhase(p) {
			t.Errorf("terminalPhase(%q) = true, want false", p)
		}
	}
}

func TestParseCodeExecutionResult(t *testing.T) {
	// Empty result.
	res, err := parseCodeExecutionResult(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode != 0 || res.Stdout != "" {
		t.Errorf("expected zero result, got %+v", res)
	}

	// Full result.
	data := []byte(`{"exitCode":0,"stdout":"hello\n","stderr":"","executionTimeMs":42,"timedOut":false,"cancelled":false}`)
	res, err = parseCodeExecutionResult(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode != 0 || res.Stdout != "hello\n" || res.ExecutionTimeMs != 42 {
		t.Errorf("unexpected result: %+v", res)
	}

	// Invalid JSON.
	if _, err := parseCodeExecutionResult([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFileExt(t *testing.T) {
	tests := []struct {
		path string
		wan  string
	}{
		{"main.py", "py"},
		{"/path/to/main.go", "go"},
		{"Dockerfile", ""},
		{".hidden", ""},
		{"noextension", ""},
		{"archive.tar.gz", "gz"},
	}
	for _, tt := range tests {
		if got := fileExt(tt.path); got != tt.wan {
			t.Errorf("fileExt(%q) = %q, want %q", tt.path, got, tt.wan)
		}
	}
}

func TestShortID(t *testing.T) {
	id := shortID()
	if id == "" {
		t.Error("shortID() returned empty string")
	}
	// Call twice to ensure it doesn't panic.
	_ = shortID()
}
