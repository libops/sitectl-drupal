package drupal

import (
	"encoding/json"
	"fmt"

	"github.com/libops/sitectl-drupal/model"
)

// Node represents a Drupal node with static core fields and dynamic bundle fields.
// Core fields (nid, uuid, type, title, etc.) are always present and typed.
// Bundle-specific fields are stored in Fields and can be accessed via typed getters.
type Node struct {
	// Core fields - present on all nodes
	Nid      model.IntField             `json:"nid"`
	UUID     model.GenericField         `json:"uuid"`
	Type     model.ConfigReferenceField `json:"type"`
	Title    model.GenericField         `json:"title"`
	Status   model.BoolField            `json:"status"`
	Created  model.GenericField         `json:"created"`
	Changed  model.GenericField         `json:"changed"`
	Langcode model.GenericField         `json:"langcode"`

	// Bundle-specific fields stored as raw JSON for lazy decoding
	Fields map[string]json.RawMessage `json:"-"`

	// Registry reference for field type lookups and validation
	registry *BundleRegistry `json:"-"`
}

// SetRegistry attaches a bundle registry to this node for validation and field lookups
func (n *Node) SetRegistry(r *BundleRegistry) {
	n.registry = r
}

// Registry returns the attached bundle registry, or nil if none
func (n *Node) Registry() *BundleRegistry {
	return n.registry
}

// Validate checks if this node has all required fields for its bundle.
// Returns nil if valid or no registry is attached.
func (n *Node) Validate() []string {
	if n.registry == nil {
		return nil
	}
	return n.registry.ValidateNode(n)
}

// GetFieldType returns the type of a field on this node's bundle.
// Requires a registry to be attached.
func (n *Node) GetFieldType(fieldName string) (FieldType, bool) {
	if n.registry == nil {
		return "", false
	}
	return n.registry.GetFieldType(n.Bundle(), fieldName)
}

// GetFieldDefinition returns the full field definition for a field on this node's bundle.
// Requires a registry to be attached.
func (n *Node) GetFieldDefinition(fieldName string) (*FieldDefinition, bool) {
	if n.registry == nil {
		return nil, false
	}
	return n.registry.GetField(n.Bundle(), fieldName)
}

// Bundle returns the bundle (content type) machine name
func (n *Node) Bundle() string {
	if len(n.Type) > 0 {
		return n.Type[0].TargetId
	}
	return ""
}

// GetField returns a field value by name as the specified type.
// Returns an error if the field doesn't exist or can't be decoded.
func GetField[T any](n *Node, fieldName string) (T, error) {
	var zero T
	raw, ok := n.Fields[fieldName]
	if !ok {
		return zero, fmt.Errorf("field %q not found on node", fieldName)
	}

	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("failed to decode field %q: %w", fieldName, err)
	}
	return result, nil
}

// MustGetField is like GetField but panics on error.
// Use only when you're certain the field exists and has the correct type.
func MustGetField[T any](n *Node, fieldName string) T {
	result, err := GetField[T](n, fieldName)
	if err != nil {
		panic(err)
	}
	return result
}

// GetGenericField returns a GenericField by name (common case)
func (n *Node) GetGenericField(fieldName string) (model.GenericField, error) {
	return GetField[model.GenericField](n, fieldName)
}

// GetEntityReferenceField returns an EntityReferenceField by name
func (n *Node) GetEntityReferenceField(fieldName string) (model.EntityReferenceField, error) {
	return GetField[model.EntityReferenceField](n, fieldName)
}

// GetTypedTextField returns a TypedTextField by name
func (n *Node) GetTypedTextField(fieldName string) (model.TypedTextField, error) {
	return GetField[model.TypedTextField](n, fieldName)
}

// HasField checks if a field exists on the node
func (n *Node) HasField(fieldName string) bool {
	_, ok := n.Fields[fieldName]
	return ok
}

// FieldNames returns all field names present on this node
func (n *Node) FieldNames() []string {
	names := make([]string, 0, len(n.Fields))
	for name := range n.Fields {
		names = append(names, name)
	}
	return names
}

// UnmarshalJSON implements custom JSON unmarshaling to capture all fields
func (n *Node) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a map to capture all fields
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Core fields to extract
	coreFields := map[string]any{
		"nid":      &n.Nid,
		"uuid":     &n.UUID,
		"type":     &n.Type,
		"title":    &n.Title,
		"status":   &n.Status,
		"created":  &n.Created,
		"changed":  &n.Changed,
		"langcode": &n.Langcode,
	}

	// Extract core fields
	for name, ptr := range coreFields {
		if rawVal, ok := raw[name]; ok {
			if err := json.Unmarshal(rawVal, ptr); err != nil {
				return fmt.Errorf("failed to unmarshal core field %q: %w", name, err)
			}
			delete(raw, name)
		}
	}

	// Remaining fields are bundle-specific
	n.Fields = raw

	return nil
}

// MarshalJSON implements custom JSON marshaling
func (n *Node) MarshalJSON() ([]byte, error) {
	// Start with bundle fields
	result := make(map[string]any, len(n.Fields)+8)
	for k, v := range n.Fields {
		result[k] = v
	}

	// Add core fields
	result["nid"] = n.Nid
	result["uuid"] = n.UUID
	result["type"] = n.Type
	result["title"] = n.Title
	result["status"] = n.Status
	result["created"] = n.Created
	result["changed"] = n.Changed
	result["langcode"] = n.Langcode

	return json.Marshal(result)
}
