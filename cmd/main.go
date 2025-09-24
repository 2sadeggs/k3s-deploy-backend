package main

import (
	"fmt"
	"k3s-deploy-backend/internal/handlers"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	// 初始化日志
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	// 加载环境变量
	if err := godotenv.Load(); err != nil {
		logger.Warn("Failed to load .env file, using default config")
	}

	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode) // 开发阶段可改为 gin.DebugMode

	// 初始化路由
	r := gin.Default()

	// 配置CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * 3600,
	}))

	// 注册API
	r.POST("/api/ssh/test", handlers.SSHTestHandler)
	r.POST("/api/ssh/test-batch", handlers.SSHBatchTestHandler)
	r.POST("/api/k3s/deploy", handlers.K3sDeployHandler)
	r.GET("/api/k3s/progress/:taskId", handlers.K3sProgressHandler)

	// 启动服务器，明确绑定到 localhost:8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := fmt.Sprintf("127.0.0.1:%s", port)
	logger.Info("Starting server", zap.String("address", addr))
	if err := r.Run(addr); err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
	}
}
