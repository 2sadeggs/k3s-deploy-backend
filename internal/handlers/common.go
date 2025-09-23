package handlers

import (
	"k3s-deploy-backend/internal/models"
	"strconv"
	"sync"
)

var (
	nodesMap = make(map[string]models.SSHTestRequestWithID)
	nodesMu  sync.Mutex
)

func SaveNode(node models.SSHTestRequestWithID) {
	nodesMu.Lock()
	nodesMap[strconv.Itoa(node.ID)] = node
	nodesMu.Unlock()
}

func GetNodeById(nodeId string) *models.SSHTestRequestWithID {
	nodesMu.Lock()
	defer nodesMu.Unlock()
	node, exists := nodesMap[nodeId]
	if !exists {
		return nil
	}
	return &node
}
