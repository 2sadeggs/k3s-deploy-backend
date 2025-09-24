package config

import (
	"os"
	"strconv"
)

type Config struct {
	Server  ServerConfig
	K3s     K3sConfig
	SSH     SSHConfig
	Logging LoggingConfig
}

type ServerConfig struct {
	Port         int
	ReadTimeout  int
	WriteTimeout int
}

type K3sConfig struct {
	Version          string
	InstallScript    string
	DefaultNamespace string
	ComponentImages  ComponentImages
}

type ComponentImages struct {
	Database   string
	Middleware string
	App        string
}

type SSHConfig struct {
	ConnectTimeout int
	CommandTimeout int
	MaxRetries     int
}

type LoggingConfig struct {
	Level  string
	Format string
}

func LoadConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:         getEnvAsInt("SERVER_PORT", 8080),
			ReadTimeout:  getEnvAsInt("READ_TIMEOUT", 30),
			WriteTimeout: getEnvAsInt("WRITE_TIMEOUT", 30),
		},
		K3s: K3sConfig{
			Version:          getEnvAsString("K3S_VERSION", "latest"),
			InstallScript:    getEnvAsString("K3S_INSTALL_SCRIPT", "https://get.k3s.io"),
			DefaultNamespace: getEnvAsString("DEFAULT_NAMESPACE", "insuite"),
			ComponentImages: ComponentImages{
				Database:   getEnvAsString("DATABASE_IMAGE", "postgres:13"),
				Middleware: getEnvAsString("MIDDLEWARE_IMAGE", "redis:6"),
				App:        getEnvAsString("APP_IMAGE", "nginx:latest"),
			},
		},
		SSH: SSHConfig{
			ConnectTimeout: getEnvAsInt("SSH_CONNECT_TIMEOUT", 30),
			CommandTimeout: getEnvAsInt("SSH_COMMAND_TIMEOUT", 300),
			MaxRetries:     getEnvAsInt("SSH_MAX_RETRIES", 3),
		},
		Logging: LoggingConfig{
			Level:  getEnvAsString("LOG_LEVEL", "info"),
			Format: getEnvAsString("LOG_FORMAT", "text"),
		},
	}
}

func getEnvAsString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
