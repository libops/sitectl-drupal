// Script to generate bundle definitions from a Drupal config sync export.
// Usage: go run ./scripts/generate-bundles --config-sync /path/to/config/sync --output ./bundles
//
// This parses node.type.*.yml, field.field.node.*.yml, and field.storage.node.*.yml
// files and generates bundle definition YAML files compatible with sitectl-drupal.
//
// Plugins (like sitectl-isle) can use this script to generate bundle configs
// from their distribution's config sync, then embed the output in their binary.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Drupal config structures

// NodeType represents a Drupal node.type.*.yml file
type NodeType struct {
	UUID        string `yaml:"uuid"`
	LangCode    string `yaml:"langcode"`
	Status      bool   `yaml:"status"`
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
}

// FieldStorage represents a Drupal field.storage.node.*.yml file
type FieldStorage struct {
	UUID        string         `yaml:"uuid"`
	FieldName   string         `yaml:"field_name"`
	EntityType  string         `yaml:"entity_type"`
	Type        string         `yaml:"type"`
	Cardinality int            `yaml:"cardinality"` // -1 = unlimited
	Settings    map[string]any `yaml:"settings"`
}

// FieldConfig represents a Drupal field.field.node.*.yml file
type FieldConfig struct {
	UUID        string         `yaml:"uuid"`
	FieldName   string         `yaml:"field_name"`
	EntityType  string         `yaml:"entity_type"`
	Bundle      string         `yaml:"bundle"`
	Label       string         `yaml:"label"`
	Description string         `yaml:"description"`
	Required    bool           `yaml:"required"`
	FieldType   string         `yaml:"field_type"`
	Settings    map[string]any `yaml:"settings"`
}

// Output structures (our format)

type BundleConfig struct {
	Version string             `yaml:"version"`
	Bundles []BundleDefinition `yaml:"bundles"`
}

type BundleDefinition struct {
	Name        string            `yaml:"name"`
	MachineName string            `yaml:"machine_name"`
	Description string            `yaml:"description,omitempty"`
	Fields      []FieldDefinition `yaml:"fields"`
}

type FieldDefinition struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Label       string `yaml:"label,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
	Cardinality int    `yaml:"cardinality,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// Map Drupal field types to our simplified types
var fieldTypeMap = map[string]string{
	// Text fields
	"string":            "generic",
	"string_long":       "generic",
	"text":              "generic",
	"text_long":         "typed_text",
	"text_with_summary": "typed_text",

	// Numeric
	"integer": "int",
	"decimal": "generic",
	"float":   "generic",
	"boolean": "bool",

	// Reference fields
	"entity_reference": "entity_reference",
	"file":             "entity_reference",
	"image":            "entity_reference",

	// Typed relation (Islandora-specific)
	"typed_relation": "typed_relation",

	// Link
	"link": "generic",

	// Date/time
	"datetime":  "generic",
	"timestamp": "generic",
	"daterange": "generic",

	// EDTF (Controlled Access Terms)
	"edtf": "edtf",

	// Email
	"email": "email",

	// Geolocation
	"geolocation": "geolocation",
	"geofield":    "geolocation",

	// Paragraph/complex
	"entity_reference_revisions": "entity_reference",

	// Fallback
	"default": "generic",
}

func mapFieldType(drupalType string) string {
	if mapped, ok := fieldTypeMap[drupalType]; ok {
		return mapped
	}
	return "generic"
}

func main() {
	configSync := flag.String("config-sync", "", "Path to Drupal config/sync directory (required)")
	output := flag.String("output", "./api/defaults", "Output directory for bundle definitions")
	flag.Parse()

	if *configSync == "" {
		fmt.Println("Usage: go run ./scripts/generate-bundles --config-sync /path/to/config/sync")
		fmt.Println()
		fmt.Println("Example with islandora-starter-site:")
		fmt.Println("  git clone --depth 1 https://github.com/Islandora-Devops/islandora-starter-site.git /tmp/starter")
		fmt.Println("  go run ./scripts/generate-bundles --config-sync /tmp/starter/config/sync")
		os.Exit(1)
	}

	// Verify config sync directory exists
	if _, err := os.Stat(*configSync); os.IsNotExist(err) {
		log.Fatalf("Config sync directory does not exist: %s", *configSync)
	}

	// Parse all node types
	nodeTypes, err := parseNodeTypes(*configSync)
	if err != nil {
		log.Fatalf("Failed to parse node types: %v", err)
	}
	fmt.Printf("Found %d node types\n", len(nodeTypes))

	// Parse all field storage configs
	fieldStorage, err := parseFieldStorage(*configSync)
	if err != nil {
		log.Fatalf("Failed to parse field storage: %v", err)
	}
	fmt.Printf("Found %d field storage definitions\n", len(fieldStorage))

	// Parse all field configs
	fieldConfigs, err := parseFieldConfigs(*configSync)
	if err != nil {
		log.Fatalf("Failed to parse field configs: %v", err)
	}
	fmt.Printf("Found %d field configurations\n", len(fieldConfigs))

	// Group field configs by bundle
	fieldsByBundle := make(map[string][]FieldConfig)
	for _, fc := range fieldConfigs {
		fieldsByBundle[fc.Bundle] = append(fieldsByBundle[fc.Bundle], fc)
	}

	// Build bundle definitions
	var bundles []BundleDefinition
	for _, nt := range nodeTypes {
		bundle := BundleDefinition{
			Name:        nt.Name,
			MachineName: nt.Type,
			Description: nt.Description,
		}

		// Add fields for this bundle
		fields := fieldsByBundle[nt.Type]
		sort.Slice(fields, func(i, j int) bool {
			return fields[i].FieldName < fields[j].FieldName
		})

		for _, fc := range fields {
			// Get cardinality from storage
			cardinality := 1
			if storage, ok := fieldStorage[fc.FieldName]; ok {
				cardinality = storage.Cardinality
			}

			fieldDef := FieldDefinition{
				Name:        fc.FieldName,
				Type:        mapFieldType(fc.FieldType),
				Label:       fc.Label,
				Required:    fc.Required,
				Description: fc.Description,
			}

			// Only include cardinality if not single-value
			if cardinality != 1 {
				fieldDef.Cardinality = cardinality
			}

			bundle.Fields = append(bundle.Fields, fieldDef)
		}

		bundles = append(bundles, bundle)
	}

	// Sort bundles by machine name
	sort.Slice(bundles, func(i, j int) bool {
		return bundles[i].MachineName < bundles[j].MachineName
	})

	// Create output directory
	if err := os.MkdirAll(*output, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Write single combined file
	config := BundleConfig{
		Version: "1.0",
		Bundles: bundles,
	}

	outputPath := filepath.Join(*output, "islandora_starter_site.yaml")
	data, err := yaml.Marshal(config)
	if err != nil {
		log.Fatalf("Failed to marshal YAML: %v", err)
	}

	// Add header comment
	header := `# Auto-generated from Islandora Starter Site config/sync
# Source: https://github.com/Islandora-Devops/islandora-starter-site
# Regenerate with: go run ./scripts/generate-bundles --config-sync /path/to/config/sync
#
`
	data = append([]byte(header), data...)

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Fatalf("Failed to write output file: %v", err)
	}

	fmt.Printf("Generated %s with %d bundles\n", outputPath, len(bundles))

	// Print summary
	fmt.Println("\nBundles:")
	for _, b := range bundles {
		fmt.Printf("  - %s (%s): %d fields\n", b.Name, b.MachineName, len(b.Fields))
	}
}

func parseNodeTypes(configSync string) ([]NodeType, error) {
	pattern := filepath.Join(configSync, "node.type.*.yml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var nodeTypes []NodeType
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}

		var nt NodeType
		if err := yaml.Unmarshal(data, &nt); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f, err)
		}

		nodeTypes = append(nodeTypes, nt)
	}

	return nodeTypes, nil
}

func parseFieldStorage(configSync string) (map[string]FieldStorage, error) {
	pattern := filepath.Join(configSync, "field.storage.node.*.yml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	storage := make(map[string]FieldStorage)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}

		var fs FieldStorage
		if err := yaml.Unmarshal(data, &fs); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f, err)
		}

		storage[fs.FieldName] = fs
	}

	return storage, nil
}

func parseFieldConfigs(configSync string) ([]FieldConfig, error) {
	pattern := filepath.Join(configSync, "field.field.node.*.yml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var configs []FieldConfig
	for _, f := range files {
		// Extract bundle and field name from filename
		// Format: field.field.node.BUNDLE.FIELD_NAME.yml
		base := filepath.Base(f)
		base = strings.TrimSuffix(base, ".yml")
		parts := strings.Split(base, ".")
		if len(parts) < 5 {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}

		var fc FieldConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f, err)
		}

		configs = append(configs, fc)
	}

	return configs, nil
}
