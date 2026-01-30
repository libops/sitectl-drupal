package model

import (
	"encoding/json"
	"log/slog"
	"strings"
)

type PartDetailField []PartDetail
type PartDetail struct {
	Type    string `json:"type,omitempty"`
	Caption string `json:"caption,omitempty"`
	Number  string `json:"number,omitempty"`
	Title   string `json:"title,omitempty"`
}

func (field *PartDetail) String() string {
	data, err := json.Marshal(field)
	if err != nil {
		slog.Error("Unable to marshal PartDetail string", "err", err)
		return ""
	}

	return string(data)
}

func (field PartDetailField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *PartDetailField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]PartDetail, len(values))
	for i, value := range values {
		var f PartDetail
		err := json.Unmarshal([]byte(value), &f)
		if err != nil {
			return err
		}
		s[i] = f
	}
	return nil
}
