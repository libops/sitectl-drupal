package drupal

import (
	"encoding/json"
	"fmt"

	"github.com/libops/sitectl-drupal/model"
)

// Term represents a Drupal taxonomy term with static core fields and dynamic vocabulary fields.
type Term struct {
	// Core fields - present on all terms
	Tid      model.IntField             `json:"tid"`
	UUID     model.GenericField         `json:"uuid"`
	Vid      model.ConfigReferenceField `json:"vid"` // vocabulary reference
	Name     model.GenericField         `json:"name"`
	Status   model.BoolField            `json:"status"`
	Created  model.GenericField         `json:"created"`
	Changed  model.GenericField         `json:"changed"`
	Langcode model.GenericField         `json:"langcode"`
	Weight   model.IntField             `json:"weight"`
	Parent   model.EntityReferenceField `json:"parent"` // parent term reference

	// Vocabulary-specific fields stored as raw JSON for lazy decoding
	Fields map[string]json.RawMessage `json:"-"`

	// Registry reference for field type lookups and validation
	registry *BundleRegistry `json:"-"`
}

// SetRegistry attaches a bundle registry to this term
func (t *Term) SetRegistry(r *BundleRegistry) {
	t.registry = r
}

// Registry returns the attached bundle registry, or nil if none
func (t *Term) Registry() *BundleRegistry {
	return t.registry
}

// Validate checks if this term has all required fields for its vocabulary.
func (t *Term) Validate() []string {
	if t.registry == nil {
		return nil
	}
	return t.registry.ValidateEntity(EntityTypeTaxonomyTerm, t.Vocabulary(), t.HasField)
}

// GetFieldType returns the type of a field on this term's vocabulary.
func (t *Term) GetFieldType(fieldName string) (FieldType, bool) {
	if t.registry == nil {
		return "", false
	}
	return t.registry.GetFieldType(EntityTypeTaxonomyTerm, t.Vocabulary(), fieldName)
}

// GetFieldDefinition returns the full field definition for a field.
func (t *Term) GetFieldDefinition(fieldName string) (*FieldDefinition, bool) {
	if t.registry == nil {
		return nil, false
	}
	return t.registry.GetField(EntityTypeTaxonomyTerm, t.Vocabulary(), fieldName)
}

// EntityType returns the entity type for terms
func (t *Term) EntityType() EntityType {
	return EntityTypeTaxonomyTerm
}

// Vocabulary returns the vocabulary (bundle) machine name
func (t *Term) Vocabulary() string {
	if len(t.Vid) > 0 {
		return t.Vid[0].TargetId
	}
	return ""
}

// Bundle is an alias for Vocabulary for interface consistency
func (t *Term) Bundle() string {
	return t.Vocabulary()
}

// GetField returns a field value by name as the specified type.
func GetTermField[T any](t *Term, fieldName string) (T, error) {
	var zero T
	raw, ok := t.Fields[fieldName]
	if !ok {
		return zero, fmt.Errorf("field %q not found on term", fieldName)
	}

	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("failed to decode field %q: %w", fieldName, err)
	}
	return result, nil
}

// GetGenericField returns a GenericField by name
func (t *Term) GetGenericField(fieldName string) (model.GenericField, error) {
	return GetTermField[model.GenericField](t, fieldName)
}

// GetEntityReferenceField returns an EntityReferenceField by name
func (t *Term) GetEntityReferenceField(fieldName string) (model.EntityReferenceField, error) {
	return GetTermField[model.EntityReferenceField](t, fieldName)
}

// HasField checks if a field exists on the term
func (t *Term) HasField(fieldName string) bool {
	_, ok := t.Fields[fieldName]
	return ok
}

// FieldNames returns all field names present on this term
func (t *Term) FieldNames() []string {
	names := make([]string, 0, len(t.Fields))
	for name := range t.Fields {
		names = append(names, name)
	}
	return names
}

// UnmarshalJSON implements custom JSON unmarshaling to capture all fields
func (t *Term) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	coreFields := map[string]any{
		"tid":      &t.Tid,
		"uuid":     &t.UUID,
		"vid":      &t.Vid,
		"name":     &t.Name,
		"status":   &t.Status,
		"created":  &t.Created,
		"changed":  &t.Changed,
		"langcode": &t.Langcode,
		"weight":   &t.Weight,
		"parent":   &t.Parent,
	}

	for name, ptr := range coreFields {
		if rawVal, ok := raw[name]; ok {
			if err := json.Unmarshal(rawVal, ptr); err != nil {
				return fmt.Errorf("failed to unmarshal core field %q: %w", name, err)
			}
			delete(raw, name)
		}
	}

	t.Fields = raw
	return nil
}

// MarshalJSON implements custom JSON marshaling
func (t *Term) MarshalJSON() ([]byte, error) {
	result := make(map[string]any, len(t.Fields)+10)
	for k, v := range t.Fields {
		result[k] = v
	}

	result["tid"] = t.Tid
	result["uuid"] = t.UUID
	result["vid"] = t.Vid
	result["name"] = t.Name
	result["status"] = t.Status
	result["created"] = t.Created
	result["changed"] = t.Changed
	result["langcode"] = t.Langcode
	result["weight"] = t.Weight
	result["parent"] = t.Parent

	return json.Marshal(result)
}
