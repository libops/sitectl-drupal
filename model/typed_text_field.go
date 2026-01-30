package model

import (
	"encoding/json"
	"log/slog"
	"strings"
)

type TypedTextField []TypedText
type TypedText struct {
	Attr0  string `json:"attr0,omitempty"`
	Attr1  string `json:"attr1,omitempty"`
	Format string `json:"format,omitempty"`
	Value  string `json:"value"`
}

func (field *TypedText) String() string {
	data, err := json.Marshal(field)
	if err != nil {
		slog.Error("Unable to marshal PartDetail string", "err", err)
		return ""
	}

	return string(data)
}

func (field *TypedTextField) String() string {
	values := make([]string, len(*field))
	for i, field := range *field {
		values[i] = field.String()
	}

	return strings.Join(values, "|")
}

func (field TypedTextField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *TypedTextField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]TypedText, len(values))
	for i, value := range values {
		var f TypedText
		err := json.Unmarshal([]byte(value), &f)
		if err != nil {
			return err
		}
		s[i] = f
	}
	return nil
}
