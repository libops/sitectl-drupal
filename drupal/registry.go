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

// EntityType represents a Drupal entity type
type EntityType string

const (
	EntityTypeNode         EntityType = "node"
	EntityTypeTaxonomyTerm EntityType = "taxonomy_term"
	EntityTypeMedia        EntityType = "media"
	EntityTypeUser         EntityType = "user"
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
	FieldTypeFile            FieldType = "file"
	FieldTypeImage           FieldType = "image"
	FieldTypeLink            FieldType = "link"
	FieldTypePassword        FieldType = "password"
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

// BundleDefinition describes a Drupal bundle (content type, vocabulary, media type, etc.)
type BundleDefinition struct {
	EntityType  EntityType        `yaml:"entity_type"`
	Name        string            `yaml:"name"`
	MachineName string            `yaml:"machine_name"`
	Description string            `yaml:"description,omitempty"`
	Fields      []FieldDefinition `yaml:"fields"`
}

// EntityConfig is the top-level config file format
type EntityConfig struct {
	Version  string             `yaml:"version"`
	Entities []BundleDefinition `yaml:"entities"`
	// Legacy support: "bundles" is an alias for "entities" with entity_type=node
	Bundles []BundleDefinition `yaml:"bundles,omitempty"`
}

// BundleRegistry manages bundle definitions from multiple sources.
// Plugins can register their own bundles using the various Load* methods.
type BundleRegistry struct {
	// bundles[entity_type][machine_name] = definition
	bundles map[EntityType]map[string]*BundleDefinition
	// fields[entity_type][bundle_name][field_name] = definition
	fields map[EntityType]map[string]map[string]*FieldDefinition
}

// NewBundleRegistry creates an empty registry.
// Use the Load* methods to populate it with bundle definitions.
func NewBundleRegistry() *BundleRegistry {
	r := &BundleRegistry{
		bundles: make(map[EntityType]map[string]*BundleDefinition),
		fields:  make(map[EntityType]map[string]map[string]*FieldDefinition),
	}
	// Initialize maps for known entity types
	for _, et := range []EntityType{EntityTypeNode, EntityTypeTaxonomyTerm, EntityTypeMedia, EntityTypeUser} {
		r.bundles[et] = make(map[string]*BundleDefinition)
		r.fields[et] = make(map[string]map[string]*FieldDefinition)
	}
	return r
}

// LoadEmbedded loads bundle definitions from an embedded filesystem.
// This is the primary mechanism for plugins to ship their own bundle configs.
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
	var config EntityConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parsing %s: %w", source, err)
	}

	// Load entities
	for i := range config.Entities {
		bundle := &config.Entities[i]
		r.RegisterBundle(bundle)
	}

	// Legacy support: "bundles" without entity_type defaults to node
	for i := range config.Bundles {
		bundle := &config.Bundles[i]
		if bundle.EntityType == "" {
			bundle.EntityType = EntityTypeNode
		}
		r.RegisterBundle(bundle)
	}

	return nil
}

// RegisterBundle adds or updates a bundle definition in the registry.
func (r *BundleRegistry) RegisterBundle(bundle *BundleDefinition) {
	et := bundle.EntityType
	if et == "" {
		et = EntityTypeNode // default
	}

	// Ensure maps exist for this entity type
	if r.bundles[et] == nil {
		r.bundles[et] = make(map[string]*BundleDefinition)
	}
	if r.fields[et] == nil {
		r.fields[et] = make(map[string]map[string]*FieldDefinition)
	}

	r.bundles[et][bundle.MachineName] = bundle
	r.fields[et][bundle.MachineName] = make(map[string]*FieldDefinition)

	for i := range bundle.Fields {
		field := &bundle.Fields[i]
		r.fields[et][bundle.MachineName][field.Name] = field
	}
}

// GetBundle returns a bundle definition by entity type and machine name
func (r *BundleRegistry) GetBundle(entityType EntityType, machineName string) (*BundleDefinition, bool) {
	if r.bundles[entityType] == nil {
		return nil, false
	}
	b, ok := r.bundles[entityType][machineName]
	return b, ok
}

// GetNodeBundle is a convenience method for GetBundle(EntityTypeNode, name)
func (r *BundleRegistry) GetNodeBundle(machineName string) (*BundleDefinition, bool) {
	return r.GetBundle(EntityTypeNode, machineName)
}

// GetTermBundle is a convenience method for GetBundle(EntityTypeTaxonomyTerm, name)
func (r *BundleRegistry) GetTermBundle(machineName string) (*BundleDefinition, bool) {
	return r.GetBundle(EntityTypeTaxonomyTerm, machineName)
}

// GetMediaBundle is a convenience method for GetBundle(EntityTypeMedia, name)
func (r *BundleRegistry) GetMediaBundle(machineName string) (*BundleDefinition, bool) {
	return r.GetBundle(EntityTypeMedia, machineName)
}

// GetUserBundle returns the user "bundle" (users don't have bundles, but have fields)
func (r *BundleRegistry) GetUserBundle() (*BundleDefinition, bool) {
	return r.GetBundle(EntityTypeUser, "user")
}

// GetField returns a field definition for an entity type and bundle
func (r *BundleRegistry) GetField(entityType EntityType, bundleName, fieldName string) (*FieldDefinition, bool) {
	if r.fields[entityType] == nil {
		return nil, false
	}
	fields, ok := r.fields[entityType][bundleName]
	if !ok {
		return nil, false
	}
	f, ok := fields[fieldName]
	return f, ok
}

// GetFieldType returns the type of a field
func (r *BundleRegistry) GetFieldType(entityType EntityType, bundleName, fieldName string) (FieldType, bool) {
	f, ok := r.GetField(entityType, bundleName, fieldName)
	if !ok {
		return "", false
	}
	return f.Type, true
}

// ListBundles returns all registered bundle machine names for an entity type
func (r *BundleRegistry) ListBundles(entityType EntityType) []string {
	if r.bundles[entityType] == nil {
		return nil
	}
	names := make([]string, 0, len(r.bundles[entityType]))
	for name := range r.bundles[entityType] {
		names = append(names, name)
	}
	return names
}

// ListNodeBundles is a convenience method for ListBundles(EntityTypeNode)
func (r *BundleRegistry) ListNodeBundles() []string {
	return r.ListBundles(EntityTypeNode)
}

// ListTermBundles is a convenience method for ListBundles(EntityTypeTaxonomyTerm)
func (r *BundleRegistry) ListTermBundles() []string {
	return r.ListBundles(EntityTypeTaxonomyTerm)
}

// ListMediaBundles is a convenience method for ListBundles(EntityTypeMedia)
func (r *BundleRegistry) ListMediaBundles() []string {
	return r.ListBundles(EntityTypeMedia)
}

// ListFields returns all field names for an entity type and bundle
func (r *BundleRegistry) ListFields(entityType EntityType, bundleName string) []string {
	if r.fields[entityType] == nil {
		return nil
	}
	fields, ok := r.fields[entityType][bundleName]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}

// ListAllEntityTypes returns all entity types that have registered bundles
func (r *BundleRegistry) ListAllEntityTypes() []EntityType {
	var types []EntityType
	for et, bundles := range r.bundles {
		if len(bundles) > 0 {
			types = append(types, et)
		}
	}
	return types
}

// Merge combines another registry into this one.
func (r *BundleRegistry) Merge(other *BundleRegistry) {
	for et := range other.bundles {
		for _, name := range other.ListBundles(et) {
			if def, ok := other.GetBundle(et, name); ok {
				r.RegisterBundle(def)
			}
		}
	}
}

// ValidateEntity checks if an entity has all required fields for its bundle.
// Works for any entity type that implements the EntityWithBundle interface.
func (r *BundleRegistry) ValidateEntity(entityType EntityType, bundleName string, hasField func(string) bool) []string {
	var errors []string

	def, ok := r.GetBundle(entityType, bundleName)
	if !ok {
		return nil // Unknown bundle - can't validate
	}

	for _, field := range def.Fields {
		if field.Required && !hasField(field.Name) {
			errors = append(errors, fmt.Sprintf("missing required field %q", field.Name))
		}
	}

	return errors
}
