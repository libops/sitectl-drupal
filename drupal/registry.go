// Package drupal provides types and utilities for interacting with Drupal APIs.
// It is designed to be extended by distribution-specific plugins (like Islandora)
// that can register their own bundle definitions.
package drupal

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FieldType represents the type of a Drupal field
type FieldType string

const (
	FieldTypeGeneric         FieldType = "generic"
	FieldTypeInt             FieldType = "int"
	FieldTypeBool            FieldType = "bool"
	FieldTypeEmail           FieldType = "email"
	FieldTypeEdtf            FieldType = "edtf"
	FieldTypeEntityReference FieldType = "entity_reference"
	FieldTypeConfigReference FieldType = "config_reference"
	FieldTypeTypedText       FieldType = "typed_text"
	FieldTypeTypedRelation   FieldType = "typed_relation"
	FieldTypeGeoLocation     FieldType = "geolocation"
	FieldTypePartDetail      FieldType = "part_detail"
	FieldTypeHierarchical    FieldType = "hierarchical_geographic"
	FieldTypeRelatedItem     FieldType = "related_item"
)

// FieldDefinition describes a field on a bundle
type FieldDefinition struct {
	Name        string    `yaml:"name"`
	Type        FieldType `yaml:"type"`
	Label       string    `yaml:"label,omitempty"`
	Required    bool      `yaml:"required,omitempty"`
	Cardinality int       `yaml:"cardinality,omitempty"` // -1 = unlimited, 1 = single, N = max N
	Description string    `yaml:"description,omitempty"`
}

// BundleDefinition describes a Drupal content type (bundle)
type BundleDefinition struct {
	Name        string            `yaml:"name"`
	MachineName string            `yaml:"machine_name"`
	Description string            `yaml:"description,omitempty"`
	Fields      []FieldDefinition `yaml:"fields"`
}

// BundleConfig is the top-level config file format
type BundleConfig struct {
	Version string             `yaml:"version"`
	Bundles []BundleDefinition `yaml:"bundles"`
}

// BundleRegistry manages bundle definitions from multiple sources.
// Plugins can register their own bundles using the various Load* methods.
type BundleRegistry struct {
	bundles map[string]*BundleDefinition            // keyed by machine_name
	fields  map[string]map[string]*FieldDefinition // bundle -> field_name -> definition
}

// NewBundleRegistry creates an empty registry.
// Use the Load* methods to populate it with bundle definitions.
func NewBundleRegistry() *BundleRegistry {
	return &BundleRegistry{
		bundles: make(map[string]*BundleDefinition),
		fields:  make(map[string]map[string]*FieldDefinition),
	}
}

// LoadEmbedded loads bundle definitions from an embedded filesystem.
// This is the primary mechanism for plugins to ship their own bundle configs.
//
// Example usage in a plugin:
//
//	//go:embed bundles/*.yaml
//	var bundleFS embed.FS
//
//	func init() {
//	    registry.LoadEmbedded(bundleFS, "bundles")
//	}
func (r *BundleRegistry) LoadEmbedded(fsys embed.FS, dir string) error {
	return fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		data, err := fsys.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		return r.loadYAML(data, path)
	})
}

// LoadFromPath loads bundle definitions from a file or directory path.
// This allows users to supply custom configs at runtime.
func (r *BundleRegistry) LoadFromPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if info.IsDir() {
		return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".yaml") && !strings.HasSuffix(p, ".yml") {
				return nil
			}
			return r.loadFile(p)
		})
	}

	return r.loadFile(path)
}

// LoadFromBytes loads bundle definitions from raw YAML bytes.
// Useful for testing or programmatic bundle registration.
func (r *BundleRegistry) LoadFromBytes(data []byte) error {
	return r.loadYAML(data, "<bytes>")
}

func (r *BundleRegistry) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return r.loadYAML(data, path)
}

func (r *BundleRegistry) loadYAML(data []byte, source string) error {
	var config BundleConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parsing %s: %w", source, err)
	}

	for i := range config.Bundles {
		bundle := &config.Bundles[i]
		r.RegisterBundle(bundle)
	}

	return nil
}

// RegisterBundle adds or updates a bundle definition in the registry.
// If a bundle with the same machine name exists, it will be replaced.
// This allows plugins to override base bundle definitions.
func (r *BundleRegistry) RegisterBundle(bundle *BundleDefinition) {
	r.bundles[bundle.MachineName] = bundle
	r.fields[bundle.MachineName] = make(map[string]*FieldDefinition)

	for i := range bundle.Fields {
		field := &bundle.Fields[i]
		r.fields[bundle.MachineName][field.Name] = field
	}
}

// GetBundle returns a bundle definition by machine name
func (r *BundleRegistry) GetBundle(machineName string) (*BundleDefinition, bool) {
	b, ok := r.bundles[machineName]
	return b, ok
}

// GetField returns a field definition for a bundle
func (r *BundleRegistry) GetField(bundleName, fieldName string) (*FieldDefinition, bool) {
	fields, ok := r.fields[bundleName]
	if !ok {
		return nil, false
	}
	f, ok := fields[fieldName]
	return f, ok
}

// GetFieldType returns the type of a field on a bundle
func (r *BundleRegistry) GetFieldType(bundleName, fieldName string) (FieldType, bool) {
	f, ok := r.GetField(bundleName, fieldName)
	if !ok {
		return "", false
	}
	return f.Type, true
}

// ListBundles returns all registered bundle machine names
func (r *BundleRegistry) ListBundles() []string {
	names := make([]string, 0, len(r.bundles))
	for name := range r.bundles {
		names = append(names, name)
	}
	return names
}

// ListFields returns all field names for a bundle
func (r *BundleRegistry) ListFields(bundleName string) []string {
	fields, ok := r.fields[bundleName]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}

// ValidateNode checks if a node has all required fields for its bundle
func (r *BundleRegistry) ValidateNode(n *Node) []string {
	var errors []string
	bundle := n.Bundle()

	def, ok := r.GetBundle(bundle)
	if !ok {
		// Unknown bundle - can't validate
		return nil
	}

	for _, field := range def.Fields {
		if field.Required && !n.HasField(field.Name) {
			errors = append(errors, fmt.Sprintf("missing required field %q", field.Name))
		}
	}

	return errors
}

// Merge combines another registry into this one.
// Bundles from the other registry will override existing bundles with the same name.
func (r *BundleRegistry) Merge(other *BundleRegistry) {
	for _, name := range other.ListBundles() {
		if def, ok := other.GetBundle(name); ok {
			r.RegisterBundle(def)
		}
	}
}
