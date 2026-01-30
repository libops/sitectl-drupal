package model

import "strings"

type EdtfField []Edtf

type Edtf struct {
	Value string `json:"value"`
}

func (field EdtfField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *EdtfField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]Edtf, len(values))
	for i, value := range values {
		s[i] = Edtf{
			Value: value,
		}
	}
	return nil
}

func (field *Edtf) String() string {
	return field.Value
}
