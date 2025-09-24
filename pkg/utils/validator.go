package utils

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func ValidateIP(ip string) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("无效的IP地址: %s", ip)
	}
	return nil
}

func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("端口必须在1-65535范围内: %d", port)
	}
	return nil
}

func ValidateNodeName(name string) error {
	if name == "" {
		return fmt.Errorf("节点名称不能为空")
	}

	if len(name) > 63 {
		return fmt.Errorf("节点名称长度不能超过63个字符")
	}

	// Kubernetes节点名称规则：只能包含小写字母、数字和连字符
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-') {
			return fmt.Errorf("节点名称只能包含小写字母、数字和连字符: %s", name)
		}
	}

	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("节点名称不能以连字符开头或结尾: %s", name)
	}

	return nil
}

func ValidatePrivateKey(privateKey string) error {
	if privateKey == "" {
		return fmt.Errorf("私钥不能为空")
	}

	if !strings.Contains(privateKey, "BEGIN") || !strings.Contains(privateKey, "END") {
		return fmt.Errorf("私钥格式无效，必须是PEM格式")
	}

	return nil
}

func SanitizeString(input string) string {
	// 移除潜在的命令注入字符
	dangerous := []string{";", "&", "|", "`", "$", "(", ")", "<", ">", "\"", "'"}
	result := input

	for _, char := range dangerous {
		result = strings.ReplaceAll(result, char, "")
	}

	return strings.TrimSpace(result)
}

func ParseNodePort(nodePort string) (int, error) {
	port, err := strconv.Atoi(nodePort)
	if err != nil {
		return 0, fmt.Errorf("解析节点端口失败: %v", err)
	}

	if err := ValidatePort(port); err != nil {
		return 0, err
	}

	return port, nil
}
