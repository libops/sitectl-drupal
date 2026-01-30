package model

import (
	"strconv"
	"strings"
)

type IntField []Int
type Int struct {
	Value int `json:"value"`
}

func (field IntField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field IntField) String() string {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|")
}

func (field *IntField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]Int, len(values))
	for i, value := range values {
		id, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		s[i] = Int{
			Value: id,
		}
	}
	return nil
}

func (field *Int) String() string {
	return strconv.Itoa(field.Value)
}
