package model

type Node struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	AuthType  string `json:"authType"`
	Connected bool   `json:"connected"`
}

type ClusterInfo struct {
	MasterNode string            `json:"masterNode"`
	AgentNodes []string          `json:"agentNodes"`
	Labels     map[string]string `json:"labels"`
	Token      string            `json:"token"`
}
