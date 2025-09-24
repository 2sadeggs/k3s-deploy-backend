package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"k3s-deploy-backend/internal/model"
	"k3s-deploy-backend/internal/service"
)

type K3sHandler struct {
	deployService *service.DeployService
}

func NewK3sHandler(deployService *service.DeployService) *K3sHandler {
	return &K3sHandler{
		deployService: deployService,
	}
}

func (h *K3sHandler) Deploy(c *gin.Context) {
	var req model.DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse{
			Success: false,
			Message: "请求参数无效",
			Details: err.Error(),
		})
		return
	}

	result := h.deployService.ExecuteStep(&req)
	c.JSON(http.StatusOK, result)
}
