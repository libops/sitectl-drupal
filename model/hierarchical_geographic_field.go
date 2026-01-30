package model

import (
	"encoding/json"
	"log/slog"
	"strings"
)

type HierarchicalGeographicField []HierarchicalGeographic
type HierarchicalGeographic struct {
	City      string `json:"city,omitempty"`
	Continent string `json:"continent,omitempty"`
	Country   string `json:"country,omitempty"`
	County    string `json:"county,omitempty"`
	State     string `json:"state,omitempty"`
	Territory string `json:"territory,omitempty"`
}

func (field *HierarchicalGeographic) String() string {
	data, err := json.Marshal(field)
	if err != nil {
		slog.Error("Unable to marshal hierarchical geo string", "err", err)
		return ""
	}

	return string(data)
}

func (field HierarchicalGeographicField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *HierarchicalGeographicField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]HierarchicalGeographic, len(values))
	for i, value := range values {
		var f HierarchicalGeographic
		err := json.Unmarshal([]byte(value), &f)
		if err != nil {
			return err
		}
		s[i] = f
	}
	return nil
}
