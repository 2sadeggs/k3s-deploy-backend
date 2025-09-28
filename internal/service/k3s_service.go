package service

import (
	"fmt"
	"path/filepath"
	"strconv"
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
	const (
		requiredSpaceGB = 450
		defaultDataDir  = "/var/lib/rancher/k3s"
	)

	// 操作系统支持检测
	result, err := client.ExecuteCommand("cat /etc/os-release")
	if err != nil {
		return fmt.Errorf("节点 %s 无法获取系统信息: %v", nodeName, err)
	}
	osRelease := strings.ToLower(result.Stdout)
	supportedDistros := []string{"ubuntu", "debian", "raspbian", "rhel", "centos", "fedora", "opensuse", "suse", "alpine", "uoss", "kylin", "deepin"}
	osID := ""
	for _, line := range strings.Split(osRelease, "\n") {
		if strings.HasPrefix(line, "id=") {
			osID = strings.TrimPrefix(line, "id=")
			osID = strings.Trim(osID, "\"")
			break
		}
	}
	if osID == "" {
		return fmt.Errorf("节点 %s 无法解析操作系统 ID", nodeName)
	}
	supported := false
	for _, distro := range supportedDistros {
		if osID == distro {
			supported = true
			break
		}
	}
	if !supported {
		return fmt.Errorf("节点 %s 操作系统不支持: %s（支持的系统: %v）", nodeName, osID, supportedDistros)
	}
	s.logger.Infof("节点 %s 操作系统验证通过: %s", nodeName, osID)

	// root 权限检查
	result, err = client.ExecuteCommand("id -u")
	if err != nil {
		return fmt.Errorf("节点 %s 无法获取用户权限信息: %v", nodeName, err)
	}
	if strings.TrimSpace(result.Stdout) != "0" {
		return fmt.Errorf("节点 %s 无 root 权限: euid=%s", nodeName, strings.TrimSpace(result.Stdout))
	}
	s.logger.Infof("节点 %s root 权限验证通过", nodeName)

	// DNS 功能检查并修复
	testDomain := "www.baidu.com" // 国内环境使用 baidu.com
	result, err = client.ExecuteCommand(fmt.Sprintf("nslookup %s", testDomain))
	dnsOk := err == nil && strings.Contains(result.Stdout, "Name:")
	if !dnsOk {
		s.logger.Warnf("节点 %s 初始 DNS 解析失败，将尝试修复 /etc/resolv.conf", nodeName)
		_, err = client.ExecuteCommand("cp /etc/resolv.conf /etc/resolv.conf.backup")
		if err != nil {
			return fmt.Errorf("节点 %s 备份 /etc/resolv.conf 失败: %v", nodeName, err)
		}
		_, err = client.ExecuteCommand("echo 'nameserver 114.114.114.114' >> /etc/resolv.conf && echo 'nameserver 8.8.8.8' >> /etc/resolv.conf")
		if err != nil {
			return fmt.Errorf("节点 %s 添加 DNS 到 /etc/resolv.conf 失败: %v", nodeName, err)
		}
		result, err = client.ExecuteCommand(fmt.Sprintf("nslookup %s", testDomain))
		if err != nil || !strings.Contains(result.Stdout, "Name:") {
			return fmt.Errorf("节点 %s 修复 /etc/resolv.conf 后 DNS 仍失败: %v", nodeName, err)
		}
		s.logger.Infof("节点 %s DNS 已修复并验证通过", nodeName)
	} else {
		s.logger.Infof("节点 %s DNS 验证通过", nodeName)
	}

	// 自定义 DNS 站点解析检查
	testDomains := []string{"get.k3s.io", "rancher-mirror.rancher.cn", "registry.cn-hangzhou.aliyuncs.com", "cdn.jsdelivr.net", "ghproxy.com"}
	for _, domain := range testDomains {
		result, err = client.ExecuteCommand(fmt.Sprintf("nslookup %s", domain))
		if err != nil || !strings.Contains(result.Stdout, "Name:") {
			return fmt.Errorf("节点 %s 无法解析域名 %s: %v", nodeName, domain, err)
		}
	}
	s.logger.Infof("节点 %s 自定义 DNS 站点解析验证通过", nodeName)

	// 网络可用性检查
	result, err = client.ExecuteCommand("timeout 1 ping -c 1 223.5.5.5 > /dev/null || timeout 1 ping -c 1 114.114.114.114 > /dev/null || timeout 1 ping -c 1 8.8.8.8 > /dev/null && echo success || echo fail")
	if err != nil || strings.TrimSpace(result.Stdout) != "success" {
		return fmt.Errorf("节点 %s 网络不可用: %v", nodeName, err)
	}
	s.logger.Infof("节点 %s 网络可用性验证通过", nodeName)

	// Swap 检查并关闭
	result, err = client.ExecuteCommand("swapon -s")
	if err == nil && strings.TrimSpace(result.Stdout) != "" {
		s.logger.Warnf("节点 %s 已启用 swap，将尝试关闭", nodeName)
		_, err = client.ExecuteCommand("swapoff -a")
		if err != nil {
			return fmt.Errorf("节点 %s 临时关闭 swap 失败: %v", nodeName, err)
		}
		_, err = client.ExecuteCommand("sed -i '/swap/d' /etc/fstab")
		if err != nil {
			return fmt.Errorf("节点 %s 持久关闭 swap 失败: %v", nodeName, err)
		}
		result, err = client.ExecuteCommand("swapon -s")
		if err == nil && strings.TrimSpace(result.Stdout) != "" {
			return fmt.Errorf("节点 %s swap 关闭失败，仍有 swap 启用", nodeName)
		}
		s.logger.Infof("节点 %s swap 已成功关闭", nodeName)
	} else {
		s.logger.Infof("节点 %s Swap 验证通过", nodeName)
	}

	// nm-cloud-setup 检查并禁用（RHEL 要求）
	result, err = client.ExecuteCommand("systemctl is-active nm-cloud-setup || echo inactive")
	if err == nil && strings.TrimSpace(result.Stdout) == "active" {
		s.logger.Warnf("节点 %s nm-cloud-setup 已启用，将尝试禁用", nodeName)
		_, err = client.ExecuteCommand("systemctl disable nm-cloud-setup.service nm-cloud-setup.timer --now")
		if err != nil {
			return fmt.Errorf("节点 %s 禁用 nm-cloud-setup 失败: %v", nodeName, err)
		}
		s.logger.Infof("节点 %s nm-cloud-setup 已禁用（建议重启节点以确保生效）", nodeName)
	} else {
		s.logger.Infof("节点 %s nm-cloud-setup 未启用或未安装", nodeName)
	}

	// 防火墙检查并关闭
	isUbuntu := strings.Contains(osRelease, "ubuntu") || strings.Contains(osRelease, "debian") || strings.Contains(osRelease, "raspbian")
	isFirewalldBased := strings.Contains(osRelease, "centos") || strings.Contains(osRelease, "rhel") || strings.Contains(osRelease, "fedora") || strings.Contains(osRelease, "opensuse") || strings.Contains(osRelease, "suse")

	if isUbuntu {
		// 检查 ufw（Ubuntu/Debian/Raspberry Pi）
		result, err = client.ExecuteCommand("command -v ufw && dpkg -l ufw >/dev/null 2>&1 && ufw status || echo inactive")
		if err == nil && strings.Contains(strings.ToLower(result.Stdout), "status: active") {
			s.logger.Warnf("节点 %s ufw 已启用，将尝试关闭", nodeName)
			_, err = client.ExecuteCommand("ufw disable")
			if err != nil {
				return fmt.Errorf("节点 %s 禁用 ufw 失败: %v", nodeName, err)
			}
			result, err = client.ExecuteCommand("ufw status")
			if err == nil && strings.Contains(strings.ToLower(result.Stdout), "status: active") {
				return fmt.Errorf("节点 %s ufw 关闭失败，状态仍为 active", nodeName)
			}
			s.logger.Infof("节点 %s ufw 已成功关闭", nodeName)
		} else {
			s.logger.Infof("节点 %s ufw 未启用或未安装", nodeName)
		}
	} else if isFirewalldBased {
		// 检查 firewalld（CentOS/RHEL/Fedora/openSUSE）
		result, err = client.ExecuteCommand("command -v systemctl && rpm -q firewalld >/dev/null 2>&1 && systemctl is-active firewalld || echo inactive")
		if err == nil && strings.TrimSpace(result.Stdout) == "active" {
			s.logger.Warnf("节点 %s firewalld 已启用，将尝试关闭", nodeName)
			_, err = client.ExecuteCommand("systemctl stop firewalld")
			if err != nil {
				return fmt.Errorf("节点 %s 停止 firewalld 失败: %v", nodeName, err)
			}
			_, err = client.ExecuteCommand("systemctl disable firewalld")
			if err != nil {
				return fmt.Errorf("节点 %s 禁用 firewalld 失败: %v", nodeName, err)
			}
			result, err = client.ExecuteCommand("systemctl is-active firewalld || echo inactive")
			if err == nil && strings.TrimSpace(result.Stdout) != "inactive" {
				return fmt.Errorf("节点 %s firewalld 关闭失败，状态仍为 active", nodeName)
			}
			s.logger.Infof("节点 %s firewalld 已成功关闭", nodeName)
		} else {
			s.logger.Infof("节点 %s firewalld 未启用或未安装", nodeName)
		}
	} else {
		// 其他系统（如 Alpine、Arch）无需检查防火墙
		s.logger.Infof("节点 %s 无需检查防火墙（非 Ubuntu 或 firewalld 基于系统）", nodeName)
	}

	s.logger.Infof("节点 %s 防火墙验证通过", nodeName)

	// CPU 检查
	result, err = client.ExecuteCommand("nproc")
	if err != nil {
		return fmt.Errorf("节点 %s 无法获取 CPU 信息: %v", nodeName, err)
	}
	cpuCoresInt, convErr := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if convErr != nil {
		return fmt.Errorf("节点 %s CPU 核心数解析失败: %v", nodeName, convErr)
	}
	if cpuCoresInt < 4 {
		s.logger.Warnf("节点 %s CPU 核心数不足: %d < 4，建议增加 CPU 资源", nodeName, cpuCoresInt)
	} else {
		s.logger.Infof("节点 %s CPU 验证通过: %d 核", nodeName, cpuCoresInt)
	}

	// 内存检查
	result, err = client.ExecuteCommand("free -m | awk 'NR==2{printf \"%.0f\", $2}'")
	if err != nil || result.Stdout == "" {
		return fmt.Errorf("节点 %s 无法获取内存信息: %v", nodeName, err)
	}
	memMB, convErr := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if convErr != nil {
		return fmt.Errorf("节点 %s 内存解析失败: %v", nodeName, convErr)
	}
	if memMB < 16384 {
		s.logger.Warnf("节点 %s 内存不足: %d MB < 16384 MB，建议增加内存资源", nodeName, memMB)
	} else {
		s.logger.Infof("节点 %s 内存验证通过: %d MB", nodeName, memMB)
	}

	// 磁盘空间检查
	result, err = client.ExecuteCommand("df -h --output=source,target,avail | grep -v tmpfs")
	if err != nil {
		return fmt.Errorf("节点 %s 无法获取磁盘分区信息: %v", nodeName, err)
	}
	maxSpaceGB := float64(0)
	var maxMountPoint string
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mountPoint := fields[1]
		avail := fields[2]
		var availGB float64
		if strings.HasSuffix(avail, "G") {
			availGB, _ = strconv.ParseFloat(strings.TrimSuffix(avail, "G"), 64)
		} else if strings.HasSuffix(avail, "M") {
			availMB, _ := strconv.ParseFloat(strings.TrimSuffix(avail, "M"), 64)
			availGB = availMB / 1024
		} else if strings.HasSuffix(avail, "T") {
			availTB, _ := strconv.ParseFloat(strings.TrimSuffix(avail, "T"), 64)
			availGB = availTB * 1024
		} else {
			continue
		}
		if availGB > maxSpaceGB {
			maxSpaceGB = availGB
			maxMountPoint = mountPoint
		}
	}
	if maxMountPoint == "" {
		return fmt.Errorf("节点 %s 没有找到可用磁盘分区", nodeName)
	}
	if maxSpaceGB < 450 {
		s.logger.Warnf("节点 %s 最大分区 %s 可用空间不足: %.1fGB < 450GB，建议增加磁盘空间", nodeName, maxMountPoint, maxSpaceGB)
	} else {
		s.logger.Infof("节点 %s 最大分区 %s 可用空间: %.1fGB，满足 450GB 要求", nodeName, maxMountPoint, maxSpaceGB)
	}

	// 软连接创建
	newDataDir := filepath.Join(maxMountPoint, "rancher", "k3s")
	if maxMountPoint != "/" {
		_, err = client.ExecuteCommand(fmt.Sprintf("mkdir -p %s", newDataDir))
		if err != nil {
			return fmt.Errorf("节点 %s 创建目录 %s 失败: %v", nodeName, newDataDir, err)
		}
		s.logger.Infof("节点 %s 创建数据目录 %s 成功", nodeName, newDataDir)

		result, err = client.ExecuteCommand("stat /var/lib/rancher/k3s")
		if err == nil {
			result, err = client.ExecuteCommand("test -L /var/lib/rancher/k3s && echo symlink || echo not_symlink")
			if err == nil && strings.TrimSpace(result.Stdout) == "symlink" {
				s.logger.Warnf("节点 %s 默认数据目录 /var/lib/rancher/k3s 已为软链接，跳过创建", nodeName)
			} else {
				result, err = client.ExecuteCommand("test -d /var/lib/rancher/k3s && echo directory || echo not_directory")
				if err == nil && strings.TrimSpace(result.Stdout) == "directory" {
					s.logger.Warnf("节点 %s 默认数据目录 /var/lib/rancher/k3s 已为目录，跳过软链接创建", nodeName)
				} else {
					_, err = client.ExecuteCommand("mkdir -p /var/lib/rancher")
					if err != nil {
						return fmt.Errorf("节点 %s 创建父目录 /var/lib/rancher 失败: %v", nodeName, err)
					}
					_, err = client.ExecuteCommand(fmt.Sprintf("ln -sf %s /var/lib/rancher/k3s", newDataDir))
					if err != nil {
						return fmt.Errorf("节点 %s 创建软链接 %s -> /var/lib/rancher/k3s 失败: %v", nodeName, newDataDir, err)
					}
					s.logger.Infof("节点 %s 默认数据目录 /var/lib/rancher/k3s 已链接到 %s", nodeName, newDataDir)
				}
			}
		} else {
			_, err = client.ExecuteCommand("mkdir -p /var/lib/rancher")
			if err != nil {
				return fmt.Errorf("节点 %s 创建父目录 /var/lib/rancher 失败: %v", nodeName, err)
			}
			_, err = client.ExecuteCommand(fmt.Sprintf("ln -sf %s /var/lib/rancher/k3s", newDataDir))
			if err != nil {
				return fmt.Errorf("节点 %s 创建软链接 %s -> /var/lib/rancher/k3s 失败: %v", nodeName, newDataDir, err)
			}
			s.logger.Infof("节点 %s 默认数据目录 /var/lib/rancher/k3s 已链接到 %s", nodeName, newDataDir)
		}
	} else {
		s.logger.Infof("节点 %s 根分区满足空间要求，无需创建软链接", nodeName)
	}

	s.logger.Infof("节点 %s 所有系统要求验证通过", nodeName)
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
