package model

import "strings"

type EmailField []Email
type Email struct {
	Value string `json:"value"`
}

func (field EmailField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *EmailField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]Email, len(values))
	for i, value := range values {
		s[i] = Email{
			Value: value,
		}
	}
	return nil
}

func (field *Email) String() string {
	return field.Value
}
