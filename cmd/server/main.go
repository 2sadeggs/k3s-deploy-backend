package main

import (
	"fmt"
	"k3s-deploy-backend/internal/config"
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
	// 加载配置
	cfg := config.LoadConfig()

	// 验证配置
	if err := cfg.Validate(); err != nil {
		log.Fatalf("配置验证失败: %v", err)
	}

	// 如果是 debug 模式，打印配置
	if cfg.Logging.Level == "debug" {
		cfg.Print()
	}

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

	// CORS 配置（从配置文件读取）
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOrigins = cfg.Server.CORSOrigins
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	corsConfig.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	r.Use(cors.New(corsConfig))

	// 注册路由
	router.RegisterRoutes(r, sshHandler, k3sHandler)

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 启动服务（使用配置文件中的地址和端口）
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	appLogger.Infof("Server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
