package model

type ConfigReferenceField []ConfigReference

type ConfigReference struct {
	TargetId   string `json:"target_id"`
	TargetType string `json:"target_type"`
	TargetUuid string `json:"target_uuid"`
}
