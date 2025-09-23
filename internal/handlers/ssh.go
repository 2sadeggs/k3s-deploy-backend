package handlers

import (
	"k3s-deploy-backend/internal/models"
	"k3s-deploy-backend/internal/services"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
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

func SSHTestHandler(c *gin.Context) {
	var req models.SSHTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		zap.L().Error("Invalid SSH test request", zap.Error(err))
		c.JSON(http.StatusBadRequest, models.SSHTestResponse{
			Success: false,
			Message: "Invalid request payload",
			Details: []string{err.Error()},
		})
		return
	}

	// 为节点生成临时ID（生产环境可用UUID或数据库ID）
	nodeWithID := models.SSHTestRequestWithID{
		ID:         len(nodesMap) + 1, // 简单递增ID
		Name:       "node-" + strconv.Itoa(len(nodesMap)+1),
		IP:         req.IP,
		Port:       req.Port,
		Username:   req.Username,
		AuthType:   req.AuthType,
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
		Passphrase: req.Passphrase,
	}

	sshService := services.NewSSHService()
	success, details, err := sshService.TestConnection(&nodeWithID)

	resp := models.SSHTestResponse{Success: success}
	if err != nil {
		resp.Message = err.Error()
		resp.Details = details
	} else {
		resp.Message = "SSH connection successful"
		resp.Details = details
		SaveNode(nodeWithID) // 保存节点信息
	}
	c.JSON(http.StatusOK, resp)
}

func SSHBatchTestHandler(c *gin.Context) {
	var req struct {
		Nodes []models.SSHTestRequestWithID `json:"nodes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		zap.L().Error("Invalid batch SSH test request", zap.Error(err))
		c.JSON(http.StatusBadRequest, models.SSHTestResponse{
			Success: false,
			Message: "Invalid request payload",
			Details: []string{err.Error()},
		})
		return
	}

	var wg sync.WaitGroup
	results := make([]models.BatchSSHTestResponseItem, len(req.Nodes))
	sshService := services.NewSSHService()

	for i, node := range req.Nodes {
		wg.Add(1)
		go func(idx int, node models.SSHTestRequestWithID) {
			defer wg.Done()
			success, _, err := sshService.TestConnection(&node)
			results[idx] = models.BatchSSHTestResponseItem{
				ID:      node.ID,
				Success: success,
			}
			if err != nil {
				results[idx].Message = err.Error()
			} else {
				results[idx].Message = "SSH connection successful"
				SaveNode(node) // 保存节点信息
			}
		}(i, node)
	}
	wg.Wait()

	c.JSON(http.StatusOK, results)
}
