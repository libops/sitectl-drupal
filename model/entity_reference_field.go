package model

import (
	"strconv"
	"strings"
)

type EntityReferenceField []EntityReference
type EntityReference struct {
	TargetId   int    `json:"target_id"`
	TargetType string `json:"target_type"`
	TargetUuid string `json:"target_uuid"`
	Url        string `json:"url"`
}

func (field EntityReferenceField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *EntityReferenceField) String() string {
	values := make([]string, len(*field))
	for i, field := range *field {
		values[i] = field.String()
	}

	return strings.Join(values, "|")
}

func (field *EntityReferenceField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]EntityReference, len(values))
	for i, value := range values {
		id, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		s[i] = EntityReference{
			TargetId: id,
		}
	}
	return nil
}

func (field *EntityReference) String() string {
	return strconv.Itoa(field.TargetId)
}
