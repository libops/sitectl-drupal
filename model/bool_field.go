package model

import "strings"

type BoolField []Bool

type Bool struct {
	Value bool `json:"value"`
}

func (field *Bool) String() string {
	if field.Value {
		return "1"
	}

	return "0"
}

func (field BoolField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *BoolField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]Bool, len(values))
	for i, value := range values {
		s[i] = Bool{}
		if value == "1" {
			s[i].Value = true
		} else {
			s[i].Value = false
		}
	}
	return nil
}
