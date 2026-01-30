package model

import (
	"encoding/json"
	"log/slog"
	"strings"
)

type RelatedItemField []RelatedItem
type RelatedItem struct {
	Identifier string `json:"identifier,omitempty"`
	Title      string `json:"title,omitempty"`
	Number     string `json:"number,omitempty"`
}

func (field *RelatedItem) String() string {
	data, err := json.Marshal(field)
	if err != nil {
		slog.Error("Unable to marshal PartDetail string", "err", err)
		return ""
	}

	return string(data)
}

func (field RelatedItemField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *RelatedItemField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]RelatedItem, len(values))
	for i, value := range values {
		var f RelatedItem
		err := json.Unmarshal([]byte(value), &f)
		if err != nil {
			return err
		}
		s[i] = f
	}
	return nil
}
