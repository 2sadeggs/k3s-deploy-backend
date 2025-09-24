package k3s

import (
	"fmt"
	"strings"
	"time"

	"k3s-deploy-backend/internal/pkg/logger"
	"k3s-deploy-backend/internal/pkg/ssh"
)

type Installer struct {
	logger *logger.Logger
}

func NewInstaller(logger *logger.Logger) *Installer {
	return &Installer{
		logger: logger,
	}
}

func (i *Installer) InstallMaster(client *ssh.Client, nodeName string) error {
	i.logger.Infof("开始在节点 %s 上安装K3s Master", nodeName)

	// 检查是否已经安装K3s
	if result, err := client.ExecuteCommand("which k3s"); err == nil && result.Stdout != "" {
		i.logger.Warnf("节点 %s 已经安装了K3s，跳过安装步骤", nodeName)
		return nil
	}

	// 下载并安装K3s
	installScript := `#!/bin/bash
set -e

# 设置环境变量
export INSTALL_K3S_EXEC="--disable traefik --write-kubeconfig-mode 644"

# 下载并安装K3s
curl -sfL https://get.k3s.io | sh -

# 等待K3s启动
sleep 30

# 检查K3s状态
systemctl status k3s --no-pager -l

# 检查节点状态
kubectl get nodes

echo "K3s Master 安装完成"
`

	// 上传安装脚本
	if err := client.UploadFile(installScript, "/tmp/install-k3s-master.sh"); err != nil {
		return fmt.Errorf("上传安装脚本失败: %v", err)
	}

	// 执行安装脚本
	if _, err := client.ExecuteCommand("chmod +x /tmp/install-k3s-master.sh && /tmp/install-k3s-master.sh"); err != nil {
		return fmt.Errorf("执行K3s Master安装失败: %v", err)
	}

	// 验证安装
	if err := i.verifyMasterInstallation(client); err != nil {
		return fmt.Errorf("验证Master安装失败: %v", err)
	}

	i.logger.Infof("节点 %s K3s Master安装成功", nodeName)
	return nil
}

func (i *Installer) InstallAgent(client *ssh.Client, nodeName, masterIP, token string) error {
	i.logger.Infof("开始在节点 %s 上安装K3s Agent", nodeName)

	// 检查是否已经安装K3s
	if result, err := client.ExecuteCommand("which k3s"); err == nil && result.Stdout != "" {
		i.logger.Warnf("节点 %s 已经安装了K3s，跳过安装步骤", nodeName)
		return nil
	}

	// Agent安装脚本
	agentScript := fmt.Sprintf(`#!/bin/bash
set -e

# 设置环境变量
export K3S_URL="https://%s:6443"
export K3S_TOKEN="%s"

# 下载并安装K3s Agent
curl -sfL https://get.k3s.io | sh -

# 等待Agent启动
sleep 20

# 检查Agent状态
systemctl status k3s-agent --no-pager -l

echo "K3s Agent 安装完成"
`, masterIP, token)

	// 上传安装脚本
	if err := client.UploadFile(agentScript, "/tmp/install-k3s-agent.sh"); err != nil {
		return fmt.Errorf("上传Agent安装脚本失败: %v", err)
	}

	// 执行安装脚本
	if _, err := client.ExecuteCommand("chmod +x /tmp/install-k3s-agent.sh && /tmp/install-k3s-agent.sh"); err != nil {
		return fmt.Errorf("执行K3s Agent安装失败: %v", err)
	}

	i.logger.Infof("节点 %s K3s Agent安装成功", nodeName)
	return nil
}

func (i *Installer) verifyMasterInstallation(client *ssh.Client) error {
	// 等待K3s完全启动
	i.logger.Info("等待K3s服务启动...")
	time.Sleep(10 * time.Second)

	// 检查K3s服务状态
	result, err := client.ExecuteCommand("systemctl is-active k3s")
	if err != nil || !strings.Contains(result.Stdout, "active") {
		return fmt.Errorf("K3s服务未正常运行: %s", result.Stderr)
	}

	// 检查kubectl可用性
	result, err = client.ExecuteCommand("kubectl get nodes")
	if err != nil {
		return fmt.Errorf("kubectl命令执行失败: %v", err)
	}

	// 检查Master节点是否Ready
	if !strings.Contains(result.Stdout, "Ready") {
		return fmt.Errorf("Master节点状态异常: %s", result.Stdout)
	}

	return nil
}
