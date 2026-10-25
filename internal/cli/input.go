package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// loadBody reads a JSON or YAML file and returns its JSON-encoded contents.
// YAML is detected by file extension (.yaml/.yml) and converted to JSON so it
// can be sent to the control plane's JSON endpoints.
func loadBody(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read input file %q: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".yaml" || ext == ".yml" {
		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("parse YAML input file %q: %w", path, err)
		}
		jsonData, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("convert YAML input to JSON: %w", err)
		}
		return jsonData, nil
	}

	// Validate that JSON input is well-formed before sending.
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse JSON input file %q: %w", path, err)
	}
	return data, nil
}
