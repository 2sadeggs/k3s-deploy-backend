package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Logging LoggingConfig `yaml:"logging"`
}

type ServerConfig struct {
	Host        string   `yaml:"host"`
	Port        int      `yaml:"port"`
	CORSOrigins []string `yaml:"cors_origins"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

const configFilePath = "config.yaml"

// getDefaultConfig 返回默认配置
func getDefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:        "127.0.0.1",
			Port:        8080,
			CORSOrigins: []string{"http://localhost:3000"},
		},
		Logging: LoggingConfig{
			Level:  "debug",
			Format: "text",
			Output: "stdout",
		},
	}
}

// LoadConfig 加载配置
func LoadConfig() *Config {
	// 检查配置文件是否存在
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		// 配置文件不存在，生成默认配置文件
		fmt.Printf("配置文件 %s 不存在，正在生成默认配置...\n", configFilePath)
		cfg := getDefaultConfig()
		if err := saveConfig(cfg); err != nil {
			fmt.Printf("⚠️  生成配置文件失败: %v\n", err)
			fmt.Println("使用内存中的默认配置继续运行")
			return cfg
		}
		fmt.Printf("✓ 已生成默认配置文件: %s\n", configFilePath)
		return cfg
	}

	// 读取配置文件
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		fmt.Printf("⚠️  读取配置文件失败: %v，使用默认配置\n", err)
		return getDefaultConfig()
	}

	// 解析配置文件
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		fmt.Printf("⚠️  解析配置文件失败: %v，使用默认配置\n", err)
		return getDefaultConfig()
	}

	fmt.Printf("✓ 已加载配置文件: %s\n", configFilePath)
	return cfg
}

// saveConfig 保存配置到文件
func saveConfig(cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 添加配置文件注释
	header := `# K3s 部署工具配置文件
# 修改后需要重启服务生效

`
	content := header + string(data)

	if err := os.WriteFile(configFilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// Validate 验证配置合法性
func (c *Config) Validate() error {
	// 验证端口范围
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return ErrInvalidPort
	}

	// 验证日志级别
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true, "fatal": true, "panic": true,
	}
	if !validLevels[strings.ToLower(c.Logging.Level)] {
		return ErrInvalidLogLevel
	}

	return nil
}

// Print 打印配置（用于调试）
func (c *Config) Print() {
	fmt.Println("=== 当前配置 ===")
	fmt.Printf("Server:\n")
	fmt.Printf("  Host: %s\n", c.Server.Host)
	fmt.Printf("  Port: %d\n", c.Server.Port)
	fmt.Printf("  CORS Origins: %v\n", c.Server.CORSOrigins)
	fmt.Printf("Logging:\n")
	fmt.Printf("  Level: %s\n", c.Logging.Level)
	fmt.Printf("  Format: %s\n", c.Logging.Format)
	fmt.Printf("  Output: %s\n", c.Logging.Output)
	fmt.Println("================")
}

// 配置错误定义
var (
	ErrInvalidPort     = &ConfigError{Field: "Server.Port", Message: "端口必须在 1-65535 范围内"}
	ErrInvalidLogLevel = &ConfigError{Field: "Logging.Level", Message: "无效的日志级别"}
)

type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return e.Field + ": " + e.Message
}
