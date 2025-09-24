package main

import (
	"log"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"k3s-deploy-backend/internal/handler"
	"k3s-deploy-backend/internal/pkg/logger"
	"k3s-deploy-backend/internal/router"
	"k3s-deploy-backend/internal/service"
)

func main() {
	// 初始化日志
	appLogger := logger.NewLogger()

	// 初始化服务
	sshService := service.NewSSHService(appLogger)
	k3sService := service.NewK3sService(appLogger)
	deployService := service.NewDeployService(sshService, k3sService, appLogger)

	// 初始化处理器
	sshHandler := handler.NewSSHHandler(sshService)
	k3sHandler := handler.NewK3sHandler(deployService)

	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)

	// 创建路由
	r := gin.New()

	// 中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// CORS 配置
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"http://localhost:3000"}
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	r.Use(cors.New(config))

	// 注册路由
	router.RegisterRoutes(r, sshHandler, k3sHandler)

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	appLogger.Info("Server starting on :8080")
	if err := r.Run("127.0.0.1:8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
