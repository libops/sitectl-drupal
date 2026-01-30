// Script to generate bundle definitions from a Drupal config sync export.
// Usage: go run ./scripts/generate-bundles --config-sync /path/to/config/sync --output ./bundles
//
// This parses config files for nodes, taxonomy vocabularies, media types, and users,
// generating bundle definition YAML files compatible with sitectl-drupal.
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

// GenericType represents any Drupal type config (node.type, taxonomy.vocabulary, media.type)
type GenericType struct {
	UUID        string `yaml:"uuid"`
	LangCode    string `yaml:"langcode"`
	Status      bool   `yaml:"status"`
	Name        string `yaml:"name"`
	ID          string `yaml:"id"`   // vocabulary, media type
	Type        string `yaml:"type"` // node type
	Vid         string `yaml:"vid"`  // vocabulary id
	Description string `yaml:"description"`
}

// MachineName returns the machine name depending on the type
func (g GenericType) MachineName() string {
	if g.Type != "" {
		return g.Type
	}
	if g.Vid != "" {
		return g.Vid
	}
	if g.ID != "" {
		return g.ID
	}
	return ""
}

// FieldStorage represents a Drupal field.storage.*.yml file
type FieldStorage struct {
	UUID        string         `yaml:"uuid"`
	FieldName   string         `yaml:"field_name"`
	EntityType  string         `yaml:"entity_type"`
	Type        string         `yaml:"type"`
	Cardinality int            `yaml:"cardinality"` // -1 = unlimited
	Settings    map[string]any `yaml:"settings"`
}

// FieldConfig represents a Drupal field.field.*.yml file
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

type EntityConfig struct {
	Version  string             `yaml:"version"`
	Entities []BundleDefinition `yaml:"entities"`
}

type BundleDefinition struct {
	EntityType  string            `yaml:"entity_type"`
	Name        string            `yaml:"name"`
	MachineName string            `yaml:"machine_name"`
	Description string            `yaml:"description,omitempty"`
	Fields      []FieldDefinition `yaml:"fields,omitempty"`
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
	"integer":  "int",
	"decimal":  "generic",
	"float":    "generic",
	"boolean":  "bool",
	"list_integer": "int",
	"list_string":  "generic",

	// Reference fields
	"entity_reference": "entity_reference",
	"file":             "file",
	"image":            "image",

	// Typed relation (Islandora-specific)
	"typed_relation": "typed_relation",

	// Link
	"link": "link",

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

	// Password
	"password": "password",
}

func mapFieldType(drupalType string) string {
	if mapped, ok := fieldTypeMap[drupalType]; ok {
		return mapped
	}
	return "generic"
}

func main() {
	configSync := flag.String("config-sync", "", "Path to Drupal config/sync directory (required)")
	output := flag.String("output", "./bundles", "Output directory for bundle definitions")
	outputFile := flag.String("output-file", "entities.yaml", "Output filename")
	flag.Parse()

	if *configSync == "" {
		fmt.Println("Usage: go run ./scripts/generate-bundles --config-sync /path/to/config/sync")
		fmt.Println()
		fmt.Println("Parses Drupal config sync to generate bundle definitions for:")
		fmt.Println("  - Node types (content types)")
		fmt.Println("  - Taxonomy vocabularies")
		fmt.Println("  - Media types")
		fmt.Println("  - User fields")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  go run ./scripts/generate-bundles --config-sync /path/to/config/sync --output ./bundles")
		os.Exit(1)
	}

	if _, err := os.Stat(*configSync); os.IsNotExist(err) {
		log.Fatalf("Config sync directory does not exist: %s", *configSync)
	}

	var allEntities []BundleDefinition

	// Parse node types
	nodeTypes, nodeStorage, nodeFields := parseEntityType(*configSync, "node", "node.type.*.yml")
	fmt.Printf("Found %d node types, %d field storage, %d field configs\n", len(nodeTypes), len(nodeStorage), len(nodeFields))
	allEntities = append(allEntities, buildBundles("node", nodeTypes, nodeStorage, nodeFields)...)

	// Parse taxonomy vocabularies
	vocabTypes, vocabStorage, vocabFields := parseEntityType(*configSync, "taxonomy_term", "taxonomy.vocabulary.*.yml")
	fmt.Printf("Found %d vocabularies, %d field storage, %d field configs\n", len(vocabTypes), len(vocabStorage), len(vocabFields))
	allEntities = append(allEntities, buildBundles("taxonomy_term", vocabTypes, vocabStorage, vocabFields)...)

	// Parse media types
	mediaTypes, mediaStorage, mediaFields := parseEntityType(*configSync, "media", "media.type.*.yml")
	fmt.Printf("Found %d media types, %d field storage, %d field configs\n", len(mediaTypes), len(mediaStorage), len(mediaFields))
	allEntities = append(allEntities, buildBundles("media", mediaTypes, mediaStorage, mediaFields)...)

	// Parse user fields (users don't have types, just fields)
	userStorage, userFields := parseUserFields(*configSync)
	if len(userFields) > 0 {
		fmt.Printf("Found %d user field storage, %d user field configs\n", len(userStorage), len(userFields))
		userBundle := buildUserBundle(userStorage, userFields)
		allEntities = append(allEntities, userBundle)
	}

	// Sort entities by type then name
	sort.Slice(allEntities, func(i, j int) bool {
		if allEntities[i].EntityType != allEntities[j].EntityType {
			return allEntities[i].EntityType < allEntities[j].EntityType
		}
		return allEntities[i].MachineName < allEntities[j].MachineName
	})

	// Create output directory
	if err := os.MkdirAll(*output, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Write output file
	config := EntityConfig{
		Version:  "1.0",
		Entities: allEntities,
	}

	outputPath := filepath.Join(*output, *outputFile)
	data, err := yaml.Marshal(config)
	if err != nil {
		log.Fatalf("Failed to marshal YAML: %v", err)
	}

	header := `# Auto-generated from Drupal config/sync
# Regenerate with: go run ./scripts/generate-bundles --config-sync /path/to/config/sync
#
`
	data = append([]byte(header), data...)

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Fatalf("Failed to write output file: %v", err)
	}

	fmt.Printf("\nGenerated %s with %d entity definitions\n", outputPath, len(allEntities))

	// Print summary by entity type
	fmt.Println("\nSummary:")
	entityCounts := make(map[string]int)
	for _, e := range allEntities {
		entityCounts[e.EntityType]++
	}
	for et, count := range entityCounts {
		fmt.Printf("  %s: %d bundles\n", et, count)
	}
}

func parseEntityType(configSync, entityType, typePattern string) ([]GenericType, map[string]FieldStorage, []FieldConfig) {
	// Parse types
	pattern := filepath.Join(configSync, typePattern)
	files, _ := filepath.Glob(pattern)

	var types []GenericType
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var t GenericType
		if err := yaml.Unmarshal(data, &t); err != nil {
			continue
		}
		types = append(types, t)
	}

	// Parse field storage
	storagePattern := filepath.Join(configSync, fmt.Sprintf("field.storage.%s.*.yml", entityType))
	storageFiles, _ := filepath.Glob(storagePattern)

	storage := make(map[string]FieldStorage)
	for _, f := range storageFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var fs FieldStorage
		if err := yaml.Unmarshal(data, &fs); err != nil {
			continue
		}
		storage[fs.FieldName] = fs
	}

	// Parse field configs
	fieldPattern := filepath.Join(configSync, fmt.Sprintf("field.field.%s.*.yml", entityType))
	fieldFiles, _ := filepath.Glob(fieldPattern)

	var fields []FieldConfig
	for _, f := range fieldFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var fc FieldConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			continue
		}
		fields = append(fields, fc)
	}

	return types, storage, fields
}

func parseUserFields(configSync string) (map[string]FieldStorage, []FieldConfig) {
	// Parse user field storage
	storagePattern := filepath.Join(configSync, "field.storage.user.*.yml")
	storageFiles, _ := filepath.Glob(storagePattern)

	storage := make(map[string]FieldStorage)
	for _, f := range storageFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var fs FieldStorage
		if err := yaml.Unmarshal(data, &fs); err != nil {
			continue
		}
		storage[fs.FieldName] = fs
	}

	// Parse user field configs
	fieldPattern := filepath.Join(configSync, "field.field.user.*.yml")
	fieldFiles, _ := filepath.Glob(fieldPattern)

	var fields []FieldConfig
	for _, f := range fieldFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var fc FieldConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			continue
		}
		fields = append(fields, fc)
	}

	return storage, fields
}

func buildBundles(entityType string, types []GenericType, storage map[string]FieldStorage, fieldConfigs []FieldConfig) []BundleDefinition {
	// Group fields by bundle
	fieldsByBundle := make(map[string][]FieldConfig)
	for _, fc := range fieldConfigs {
		fieldsByBundle[fc.Bundle] = append(fieldsByBundle[fc.Bundle], fc)
	}

	var bundles []BundleDefinition
	for _, t := range types {
		machineName := t.MachineName()
		bundle := BundleDefinition{
			EntityType:  entityType,
			Name:        t.Name,
			MachineName: machineName,
			Description: cleanDescription(t.Description),
		}

		// Add fields
		fields := fieldsByBundle[machineName]
		sort.Slice(fields, func(i, j int) bool {
			return fields[i].FieldName < fields[j].FieldName
		})

		for _, fc := range fields {
			cardinality := 1
			if s, ok := storage[fc.FieldName]; ok {
				cardinality = s.Cardinality
			}

			fieldDef := FieldDefinition{
				Name:        fc.FieldName,
				Type:        mapFieldType(fc.FieldType),
				Label:       fc.Label,
				Required:    fc.Required,
				Description: cleanDescription(fc.Description),
			}

			if cardinality != 1 {
				fieldDef.Cardinality = cardinality
			}

			bundle.Fields = append(bundle.Fields, fieldDef)
		}

		bundles = append(bundles, bundle)
	}

	return bundles
}

func buildUserBundle(storage map[string]FieldStorage, fieldConfigs []FieldConfig) BundleDefinition {
	bundle := BundleDefinition{
		EntityType:  "user",
		Name:        "User",
		MachineName: "user",
		Description: "Drupal user account",
	}

	sort.Slice(fieldConfigs, func(i, j int) bool {
		return fieldConfigs[i].FieldName < fieldConfigs[j].FieldName
	})

	for _, fc := range fieldConfigs {
		cardinality := 1
		if s, ok := storage[fc.FieldName]; ok {
			cardinality = s.Cardinality
		}

		fieldDef := FieldDefinition{
			Name:        fc.FieldName,
			Type:        mapFieldType(fc.FieldType),
			Label:       fc.Label,
			Required:    fc.Required,
			Description: cleanDescription(fc.Description),
		}

		if cardinality != 1 {
			fieldDef.Cardinality = cardinality
		}

		bundle.Fields = append(bundle.Fields, fieldDef)
	}

	return bundle
}

// cleanDescription removes HTML tags and excessive whitespace
func cleanDescription(desc string) string {
	// Simple HTML tag removal - not comprehensive but handles common cases
	desc = strings.ReplaceAll(desc, "<br>", " ")
	desc = strings.ReplaceAll(desc, "<br/>", " ")
	desc = strings.ReplaceAll(desc, "<br />", " ")
	desc = strings.ReplaceAll(desc, "\r\n", " ")
	desc = strings.ReplaceAll(desc, "\n", " ")

	// Trim and collapse whitespace
	fields := strings.Fields(desc)
	return strings.Join(fields, " ")
}
