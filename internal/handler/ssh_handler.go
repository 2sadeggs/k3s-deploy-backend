package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"k3s-deploy-backend/internal/model"
	"k3s-deploy-backend/internal/service"
)

type SSHHandler struct {
	sshService *service.SSHService
}

func NewSSHHandler(sshService *service.SSHService) *SSHHandler {
	return &SSHHandler{
		sshService: sshService,
	}
}

func (h *SSHHandler) TestConnection(c *gin.Context) {
	var req model.SSHTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse{
			Success: false,
			Message: "请求参数无效",
			Details: err.Error(),
		})
		return
	}

	result := h.sshService.TestConnection(&req)
	c.JSON(http.StatusOK, result)
}

func (h *SSHHandler) BatchTestConnection(c *gin.Context) {
	var req model.BatchSSHTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse{
			Success: false,
			Message: "请求参数无效",
			Details: err.Error(),
		})
		return
	}

	results := h.sshService.BatchTestConnection(&req)
	c.JSON(http.StatusOK, results)
}
