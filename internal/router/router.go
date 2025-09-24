package router

import (
	"github.com/gin-gonic/gin"
	"k3s-deploy-backend/internal/handler"
)

func RegisterRoutes(r *gin.Engine, sshHandler *handler.SSHHandler, k3sHandler *handler.K3sHandler) {
	api := r.Group("/api")
	{
		ssh := api.Group("/ssh")
		{
			ssh.POST("/test", sshHandler.TestConnection)
			ssh.POST("/test-batch", sshHandler.BatchTestConnection)
		}

		k3s := api.Group("/k3s")
		{
			k3s.POST("/deploy", k3sHandler.Deploy)
		}
	}
}
