package drupal

import (
	"encoding/json"
	"fmt"

	"github.com/libops/sitectl-drupal/model"
)

// User represents a Drupal user entity.
// Users don't have bundles in Drupal, but they do have configurable fields.
type User struct {
	// Core fields - present on all users
	UID      model.IntField             `json:"uid"`
	UUID     model.GenericField         `json:"uuid"`
	Name     model.GenericField         `json:"name"`
	Mail     model.GenericField         `json:"mail"`
	Status   model.BoolField            `json:"status"`
	Created  model.GenericField         `json:"created"`
	Changed  model.GenericField         `json:"changed"`
	Langcode model.GenericField         `json:"langcode"`
	Timezone model.GenericField         `json:"timezone"`
	Roles    model.ConfigReferenceField `json:"roles"`

	// User fields stored as raw JSON for lazy decoding
	Fields map[string]json.RawMessage `json:"-"`

	// Registry reference for field type lookups and validation
	registry *BundleRegistry `json:"-"`
}

// SetRegistry attaches a bundle registry to this user
func (u *User) SetRegistry(r *BundleRegistry) {
	u.registry = r
}

// Registry returns the attached bundle registry, or nil if none
func (u *User) Registry() *BundleRegistry {
	return u.registry
}

// Validate checks if this user has all required fields.
// Users use "user" as their bundle name for registry lookups.
func (u *User) Validate() []string {
	if u.registry == nil {
		return nil
	}
	return u.registry.ValidateEntity(EntityTypeUser, "user", u.HasField)
}

// GetFieldType returns the type of a field on users.
func (u *User) GetFieldType(fieldName string) (FieldType, bool) {
	if u.registry == nil {
		return "", false
	}
	return u.registry.GetFieldType(EntityTypeUser, "user", fieldName)
}

// GetFieldDefinition returns the full field definition for a field.
func (u *User) GetFieldDefinition(fieldName string) (*FieldDefinition, bool) {
	if u.registry == nil {
		return nil, false
	}
	return u.registry.GetField(EntityTypeUser, "user", fieldName)
}

// EntityType returns the entity type for users
func (u *User) EntityType() EntityType {
	return EntityTypeUser
}

// Bundle returns "user" - users don't have bundles but we use this for consistency
func (u *User) Bundle() string {
	return "user"
}

// GetField returns a field value by name as the specified type.
func GetUserField[T any](u *User, fieldName string) (T, error) {
	var zero T
	raw, ok := u.Fields[fieldName]
	if !ok {
		return zero, fmt.Errorf("field %q not found on user", fieldName)
	}

	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("failed to decode field %q: %w", fieldName, err)
	}
	return result, nil
}

// GetGenericField returns a GenericField by name
func (u *User) GetGenericField(fieldName string) (model.GenericField, error) {
	return GetUserField[model.GenericField](u, fieldName)
}

// GetEntityReferenceField returns an EntityReferenceField by name
func (u *User) GetEntityReferenceField(fieldName string) (model.EntityReferenceField, error) {
	return GetUserField[model.EntityReferenceField](u, fieldName)
}

// HasField checks if a field exists on the user
func (u *User) HasField(fieldName string) bool {
	_, ok := u.Fields[fieldName]
	return ok
}

// FieldNames returns all field names present on this user
func (u *User) FieldNames() []string {
	names := make([]string, 0, len(u.Fields))
	for name := range u.Fields {
		names = append(names, name)
	}
	return names
}

// UnmarshalJSON implements custom JSON unmarshaling to capture all fields
func (u *User) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	coreFields := map[string]any{
		"uid":      &u.UID,
		"uuid":     &u.UUID,
		"name":     &u.Name,
		"mail":     &u.Mail,
		"status":   &u.Status,
		"created":  &u.Created,
		"changed":  &u.Changed,
		"langcode": &u.Langcode,
		"timezone": &u.Timezone,
		"roles":    &u.Roles,
	}

	for name, ptr := range coreFields {
		if rawVal, ok := raw[name]; ok {
			if err := json.Unmarshal(rawVal, ptr); err != nil {
				return fmt.Errorf("failed to unmarshal core field %q: %w", name, err)
			}
			delete(raw, name)
		}
	}

	u.Fields = raw
	return nil
}

// MarshalJSON implements custom JSON marshaling
func (u *User) MarshalJSON() ([]byte, error) {
	result := make(map[string]any, len(u.Fields)+10)
	for k, v := range u.Fields {
		result[k] = v
	}

	result["uid"] = u.UID
	result["uuid"] = u.UUID
	result["name"] = u.Name
	result["mail"] = u.Mail
	result["status"] = u.Status
	result["created"] = u.Created
	result["changed"] = u.Changed
	result["langcode"] = u.Langcode
	result["timezone"] = u.Timezone
	result["roles"] = u.Roles

	return json.Marshal(result)
}
