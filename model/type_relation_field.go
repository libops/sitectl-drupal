package model

import (
	"encoding/json"
	"log/slog"
	"strings"
)

type TypedRelationField []TypedRelation
type TypedRelation struct {
	TargetId int    `json:"target_id"`
	RelType  string `json:"rel_type"`
	Url      string `json:"url"`
}

func (field *TypedRelation) String() string {
	// TODO: rel:bundle:name
	data, err := json.Marshal(field)
	if err != nil {
		slog.Error("Unable to marshal PartDetail string", "err", err)
		return ""
	}

	return string(data)
}

func (field TypedRelationField) MarshalCSV() (string, error) {
	values := make([]string, len(field))
	for i, field := range field {
		values[i] = field.String()
	}
	return strings.Join(values, "|"), nil
}

func (field *TypedRelationField) UnmarshalCSV(csv string) error {
	values := strings.Split(csv, "|")
	s := make([]TypedRelation, len(values))
	for i, value := range values {
		var f TypedRelation
		err := json.Unmarshal([]byte(value), &f)
		if err != nil {
			return err
		}
		s[i] = f
	}
	return nil
}
