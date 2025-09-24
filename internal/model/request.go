package model

type SSHTestRequest struct {
	IP         string `json:"ip" binding:"required"`
	Port       int    `json:"port" binding:"required"`
	Username   string `json:"username" binding:"required"`
	AuthType   string `json:"authType" binding:"required,oneof=password key"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}

type BatchSSHTestRequest struct {
	Nodes []BatchNodeRequest `json:"nodes" binding:"required"`
}

type BatchNodeRequest struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}

type DeployRequest struct {
	DeployMode     string              `json:"deployMode" binding:"required,oneof=single dual triple"`
	Step           string              `json:"step" binding:"required"`
	Nodes          []NodeConfig        `json:"nodes" binding:"required"`
	RoleAssignment map[string]string   `json:"roleAssignment" binding:"required"`
	Labels         map[string][]string `json:"labels"`
}

type NodeConfig struct {
	Name       string `json:"name"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}
