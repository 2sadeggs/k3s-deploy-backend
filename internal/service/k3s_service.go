package service

import (
	"fmt"
	"strings"

	"k3s-deploy-backend/internal/model"
	"k3s-deploy-backend/internal/pkg/k3s"
	"k3s-deploy-backend/internal/pkg/logger"
	"k3s-deploy-backend/internal/pkg/ssh"
)

type K3sService struct {
	installer *k3s.Installer
	manager   *k3s.Manager
	logger    *logger.Logger
}

func NewK3sService(logger *logger.Logger) *K3sService {
	return &K3sService{
		installer: k3s.NewInstaller(logger),
		manager:   k3s.NewManager(logger),
		logger:    logger,
	}
}

func (s *K3sService) ValidateNodes(nodes []model.NodeConfig) error {
	s.logger.Info("开始验证节点连接状态")

	for _, node := range nodes {
		client := ssh.NewClient(ssh.SSHConfig{
			Host:       node.IP,
			Port:       node.Port,
			Username:   node.Username,
			AuthType:   node.AuthType,
			Password:   node.Password,
			PrivateKey: node.PrivateKey,
			Passphrase: node.Passphrase,
		})

		if err := client.Connect(); err != nil {
			return fmt.Errorf("节点 %s (%s) 连接失败: %v", node.Name, node.IP, err)
		}

		if err := s.checkSystemRequirements(client, node.Name); err != nil {
			client.Close()
			return fmt.Errorf("节点 %s 系统检查失败: %v", node.Name, err)
		}

		client.Close()
		s.logger.Infof("节点 %s 验证通过", node.Name)
	}

	return nil
}

func (s *K3sService) checkSystemRequirements(client *ssh.Client, nodeName string) error {
	result, err := client.ExecuteCommand("cat /etc/os-release")
	if err != nil {
		return fmt.Errorf("无法获取系统信息: %v", err)
	}

	if !strings.Contains(result.Stdout, "ubuntu") && !strings.Contains(result.Stdout, "centos") {
		s.logger.Warnf("节点 %s 操作系统可能不受支持", nodeName)
	}

	result, err = client.ExecuteCommand("free -m | awk 'NR==2{printf \"%.0f\", $2}'")
	if err == nil && result.Stdout != "" {
		s.logger.Infof("节点 %s 内存: %s MB", nodeName, result.Stdout)
	}

	return nil
}

func (s *K3sService) InstallMaster(node model.NodeConfig) error {
	s.logger.DeploymentStep("install-master", node.Name)

	client := ssh.NewClient(ssh.SSHConfig{
		Host:       node.IP,
		Port:       node.Port,
		Username:   node.Username,
		AuthType:   node.AuthType,
		Password:   node.Password,
		PrivateKey: node.PrivateKey,
		Passphrase: node.Passphrase,
	})

	if err := client.Connect(); err != nil {
		return fmt.Errorf("连接Master节点失败: %v", err)
	}
	defer client.Close()

	return s.installer.InstallMaster(client, node.Name)
}

func (s *K3sService) ConfigureAgent(masterNode, agentNode model.NodeConfig, agentIndex int) error {
	s.logger.DeploymentStep("configure-agent", agentNode.Name)

	// 获取Master节点token
	masterClient := ssh.NewClient(ssh.SSHConfig{
		Host:       masterNode.IP,
		Port:       masterNode.Port,
		Username:   masterNode.Username,
		AuthType:   masterNode.AuthType,
		Password:   masterNode.Password,
		PrivateKey: masterNode.PrivateKey,
		Passphrase: masterNode.Passphrase,
	})

	if err := masterClient.Connect(); err != nil {
		return fmt.Errorf("连接Master节点获取token失败: %v", err)
	}

	token, err := s.manager.GetNodeToken(masterClient)
	if err != nil {
		masterClient.Close()
		return fmt.Errorf("获取节点token失败: %v", err)
	}

	// 连接Agent节点
	agentClient := ssh.NewClient(ssh.SSHConfig{
		Host:       agentNode.IP,
		Port:       agentNode.Port,
		Username:   agentNode.Username,
		AuthType:   agentNode.AuthType,
		Password:   agentNode.Password,
		PrivateKey: agentNode.PrivateKey,
		Passphrase: agentNode.Passphrase,
	})

	if err := agentClient.Connect(); err != nil {
		masterClient.Close()
		return fmt.Errorf("连接Agent节点失败: %v", err)
	}
	defer agentClient.Close()

	// 动态生成Agent节点名称
	agentNodeName := "k3s-agent"
	if agentIndex > 0 {
		agentNodeName = fmt.Sprintf("k3s-agent-%d", agentIndex+1)
	}

	err = s.installer.InstallAgent(agentClient, masterClient, agentNodeName, token)
	masterClient.Close()
	if err != nil {
		return fmt.Errorf("配置Agent节点 %s 失败: %v", agentNodeName, err)
	}

	return nil
}

func (s *K3sService) ApplyLabels(masterNode model.NodeConfig, labels map[string][]string) error {
	s.logger.DeploymentStep("apply-labels", "cluster")

	client := ssh.NewClient(ssh.SSHConfig{
		Host:       masterNode.IP,
		Port:       masterNode.Port,
		Username:   masterNode.Username,
		AuthType:   masterNode.AuthType,
		Password:   masterNode.Password,
		PrivateKey: masterNode.PrivateKey,
		Passphrase: masterNode.Passphrase,
	})

	if err := client.Connect(); err != nil {
		return fmt.Errorf("连接Master节点失败: %v", err)
	}
	defer client.Close()

	return s.manager.ApplyNodeLabels(client, labels)
}

func (s *K3sService) DeployInSuite(masterNode model.NodeConfig, roleAssignment map[string]string) error {
	s.logger.DeploymentStep("deploy-insuite", "cluster")

	client := ssh.NewClient(ssh.SSHConfig{
		Host:       masterNode.IP,
		Port:       masterNode.Port,
		Username:   masterNode.Username,
		AuthType:   masterNode.AuthType,
		Password:   masterNode.Password,
		PrivateKey: masterNode.PrivateKey,
		Passphrase: masterNode.Passphrase,
	})

	if err := client.Connect(); err != nil {
		return fmt.Errorf("连接Master节点失败: %v", err)
	}
	defer client.Close()

	return s.manager.DeployInSuite(client, roleAssignment)
}

func (s *K3sService) VerifyDeployment(masterNode model.NodeConfig) error {
	s.logger.DeploymentStep("verify", "cluster")

	client := ssh.NewClient(ssh.SSHConfig{
		Host:       masterNode.IP,
		Port:       masterNode.Port,
		Username:   masterNode.Username,
		AuthType:   masterNode.AuthType,
		Password:   masterNode.Password,
		PrivateKey: masterNode.PrivateKey,
		Passphrase: masterNode.Passphrase,
	})

	if err := client.Connect(); err != nil {
		return fmt.Errorf("连接Master节点失败: %v", err)
	}
	defer client.Close()

	return s.manager.VerifyDeployment(client)
}
