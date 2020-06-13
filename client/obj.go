package client

import "github.com/freemed/remitt-server/model"

// JobStatus represents REMITT's current job status
type JobStatus struct {
	Status int    `db:"status" json:"status"`
	Stage  string `db:"stage" json:"stage"`
}

// InputPayload represents a processing payload
type InputPayload struct {
	OriginalID      model.NullString `json:"original_id"`
	InputPayload    string           `json:"input_payload"`
	RenderPlugin    string           `json:"render_plugin"`
	RenderOption    string           `json:"render_option"`
	TransportPlugin string           `json:"transport_plugin"`
	TransportOption string           `json:"transport_option"`
}
