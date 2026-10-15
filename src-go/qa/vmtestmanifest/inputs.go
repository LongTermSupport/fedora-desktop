package vmtestmanifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"
)

// readFile reads a repository file; a file that cannot be read is an error, never a skip.
func (g *gate) readFile(rel string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", rel, err)
	}
	return data, nil
}

// decodeYAML reads exactly one YAML document whose top is a mapping. A duplicate key is the
// library's error; the values must be expressible as JSON, which is what the validator is given.
func decodeYAML(name string, data []byte) (map[string]any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc any
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s holds no YAML document", name)
		}
		return nil, fmt.Errorf("%s is not valid YAML: %w", name, err)
	}
	var more any
	switch err := dec.Decode(&more); {
	case err == nil:
		return nil, fmt.Errorf("%s holds more than one YAML document", name)
	case !errors.Is(err, io.EOF):
		return nil, fmt.Errorf("%s is not valid YAML: %w", name, err)
	}
	top, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping at the top, with string keys", name)
	}
	if err := jsonable(name, doc, name); err != nil {
		return nil, err
	}
	return top, nil
}

// jsonable is nil when every value below v has a JSON form: mappings keyed by strings, finite
// numbers.
func jsonable(file string, v any, path string) error {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(x)) {
			if err := jsonable(file, x[k], path+"."+k); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range x {
			if err := jsonable(file, child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case map[any]any:
		return fmt.Errorf("%s: %s is a mapping with a key that is not a string", file, path)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Errorf("%s: %s is %v, which has no JSON form", file, path, x)
		}
	}
	return nil
}

// fedoraVersion is the integer in vars/fedora-version.yml that base names are composed with.
func (g *gate) fedoraVersion() (int, error) {
	data, err := g.readFile(versionFile)
	if err != nil {
		return 0, err
	}
	doc, err := decodeYAML(versionFile, data)
	if err != nil {
		return 0, err
	}
	version, ok := doc["fedora_version"].(int)
	if !ok || version <= 0 {
		return 0, fmt.Errorf("%s: fedora_version must be an integer greater than zero, got %v", versionFile, doc["fedora_version"])
	}
	return version, nil
}

// loadManifest reads the manifest and returns it decoded and as the JSON the validator reads.
func (g *gate) loadManifest() (map[string]any, []byte, error) {
	data, err := g.readFile(manifestFile)
	if err != nil {
		return nil, nil, err
	}
	doc, err := decodeYAML(manifestFile, data)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("%s cannot be written as JSON: %w", manifestFile, err)
	}
	return doc, encoded, nil
}
