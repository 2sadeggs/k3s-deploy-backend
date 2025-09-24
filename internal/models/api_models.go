package models

type SSHTestRequest struct {
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type SSHTestRequestWithID struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type SSHTestResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message,omitempty"`
	Details []string `json:"details,omitempty"`
}

type BatchSSHTestResponseItem struct {
	ID      int      `json:"id"`
	Success bool     `json:"success"`
	Message string   `json:"message,omitempty"`
	Details []string `json:"details,omitempty"` // 新增这一行
}
type DeployRequest struct {
	DeployMode     string                 `json:"deployMode"`
	Nodes          []SSHTestRequestWithID `json:"nodes"`
	RoleAssignment map[string]string      `json:"roleAssignment"`
	Labels         map[string][]string    `json:"labels"`
}

type DeployResponse struct {
	Success bool   `json:"success"`
	TaskID  string `json:"taskId"`
	Message string `json:"message,omitempty"`
}

type ProgressResponse struct {
	Success  bool     `json:"success"`
	Progress float64  `json:"progress"`
	Status   string   `json:"status"`
	Logs     []string `json:"logs"`
	Error    string   `json:"error,omitempty"`
}
