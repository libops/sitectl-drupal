package drupal

import (
	"encoding/json"
	"fmt"

	"github.com/libops/sitectl-drupal/model"
)

// Media represents a Drupal media entity with static core fields and dynamic media type fields.
type Media struct {
	// Core fields - present on all media
	Mid      model.IntField             `json:"mid"`
	UUID     model.GenericField         `json:"uuid"`
	Bundle   model.ConfigReferenceField `json:"bundle"` // media type reference
	Name     model.GenericField         `json:"name"`
	Status   model.BoolField            `json:"status"`
	Created  model.GenericField         `json:"created"`
	Changed  model.GenericField         `json:"changed"`
	Langcode model.GenericField         `json:"langcode"`
	UID      model.EntityReferenceField `json:"uid"` // author

	// Media type-specific fields stored as raw JSON for lazy decoding
	Fields map[string]json.RawMessage `json:"-"`

	// Registry reference for field type lookups and validation
	registry *BundleRegistry `json:"-"`
}

// SetRegistry attaches a bundle registry to this media entity
func (m *Media) SetRegistry(r *BundleRegistry) {
	m.registry = r
}

// Registry returns the attached bundle registry, or nil if none
func (m *Media) Registry() *BundleRegistry {
	return m.registry
}

// Validate checks if this media has all required fields for its media type.
func (m *Media) Validate() []string {
	if m.registry == nil {
		return nil
	}
	return m.registry.ValidateEntity(EntityTypeMedia, m.MediaType(), m.HasField)
}

// GetFieldType returns the type of a field on this media's type.
func (m *Media) GetFieldType(fieldName string) (FieldType, bool) {
	if m.registry == nil {
		return "", false
	}
	return m.registry.GetFieldType(EntityTypeMedia, m.MediaType(), fieldName)
}

// GetFieldDefinition returns the full field definition for a field.
func (m *Media) GetFieldDefinition(fieldName string) (*FieldDefinition, bool) {
	if m.registry == nil {
		return nil, false
	}
	return m.registry.GetField(EntityTypeMedia, m.MediaType(), fieldName)
}

// EntityType returns the entity type for media
func (m *Media) EntityType() EntityType {
	return EntityTypeMedia
}

// MediaType returns the media type (bundle) machine name
func (m *Media) MediaType() string {
	if len(m.Bundle) > 0 {
		return m.Bundle[0].TargetId
	}
	return ""
}

// GetField returns a field value by name as the specified type.
func GetMediaField[T any](m *Media, fieldName string) (T, error) {
	var zero T
	raw, ok := m.Fields[fieldName]
	if !ok {
		return zero, fmt.Errorf("field %q not found on media", fieldName)
	}

	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("failed to decode field %q: %w", fieldName, err)
	}
	return result, nil
}

// GetGenericField returns a GenericField by name
func (m *Media) GetGenericField(fieldName string) (model.GenericField, error) {
	return GetMediaField[model.GenericField](m, fieldName)
}

// GetEntityReferenceField returns an EntityReferenceField by name
func (m *Media) GetEntityReferenceField(fieldName string) (model.EntityReferenceField, error) {
	return GetMediaField[model.EntityReferenceField](m, fieldName)
}

// HasField checks if a field exists on the media
func (m *Media) HasField(fieldName string) bool {
	_, ok := m.Fields[fieldName]
	return ok
}

// FieldNames returns all field names present on this media
func (m *Media) FieldNames() []string {
	names := make([]string, 0, len(m.Fields))
	for name := range m.Fields {
		names = append(names, name)
	}
	return names
}

// UnmarshalJSON implements custom JSON unmarshaling to capture all fields
func (m *Media) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	coreFields := map[string]any{
		"mid":      &m.Mid,
		"uuid":     &m.UUID,
		"bundle":   &m.Bundle,
		"name":     &m.Name,
		"status":   &m.Status,
		"created":  &m.Created,
		"changed":  &m.Changed,
		"langcode": &m.Langcode,
		"uid":      &m.UID,
	}

	for name, ptr := range coreFields {
		if rawVal, ok := raw[name]; ok {
			if err := json.Unmarshal(rawVal, ptr); err != nil {
				return fmt.Errorf("failed to unmarshal core field %q: %w", name, err)
			}
			delete(raw, name)
		}
	}

	m.Fields = raw
	return nil
}

// MarshalJSON implements custom JSON marshaling
func (m *Media) MarshalJSON() ([]byte, error) {
	result := make(map[string]any, len(m.Fields)+9)
	for k, v := range m.Fields {
		result[k] = v
	}

	result["mid"] = m.Mid
	result["uuid"] = m.UUID
	result["bundle"] = m.Bundle
	result["name"] = m.Name
	result["status"] = m.Status
	result["created"] = m.Created
	result["changed"] = m.Changed
	result["langcode"] = m.Langcode
	result["uid"] = m.UID

	return json.Marshal(result)
}
