package model

type TermResponse struct {
	ID            IntField           `json:"tid"`
	Name          GenericField       `json:"name"`
	Relationships TypedRelationField `json:"field_relationships"`
	Identifier    TypedTextField     `json:"field_identifier"`
}
