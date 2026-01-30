package model

import (
	"strings"
)

type GenericField []Generic
type Generic struct {
	Format    string `json:"format,omitempty"`
	Processed string `json:"processed,omitempty"`
	Value     string `json:"value"`
}

func (field GenericField) MarshalCSV() string {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|")
}

func (field *GenericField) String() string {
	values := make([]string, len(*field))
	for i, field := range *field {
		values[i] = field.String()
	}

	return strings.Join(values, "|")
}

func (field *GenericField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]Generic, len(values))
	for i, value := range values {
		s[i] = Generic{
			Value: value,
		}
	}
	return nil
}

func (field *Generic) String() string {
	return field.Value
}
