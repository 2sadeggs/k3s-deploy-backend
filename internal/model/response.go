package model

type SSHTestResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message,omitempty"`
	Details []string `json:"details,omitempty"`
	ID      int      `json:"id,omitempty"`
}

type DeployResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Step    string `json:"step,omitempty"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}
