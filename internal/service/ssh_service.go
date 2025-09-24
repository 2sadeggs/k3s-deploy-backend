package service

import (
	"fmt"
	"sync"

	"k3s-deploy-backend/internal/model"
	"k3s-deploy-backend/internal/pkg/logger"
	"k3s-deploy-backend/internal/pkg/ssh"
)

type SSHService struct {
	logger *logger.Logger
}

func NewSSHService(logger *logger.Logger) *SSHService {
	return &SSHService{
		logger: logger,
	}
}

func (s *SSHService) TestConnection(req *model.SSHTestRequest) *model.SSHTestResponse {
	s.logger.SSHConnectionAttempt("single", req.IP)

	client := ssh.NewClient(ssh.SSHConfig{
		Host:       req.IP,
		Port:       req.Port,
		Username:   req.Username,
		AuthType:   req.AuthType,
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
		Passphrase: req.Passphrase,
	})

	if err := client.Connect(); err != nil {
		s.logger.Errorf("SSH connection failed for %s: %v", req.IP, err)
		return &model.SSHTestResponse{
			Success: false,
			Details: []string{
				"✗ SSH连接测试失败",
				fmt.Sprintf("错误信息: %s", err.Error()),
			},
		}
	}
	defer client.Close()

	// 执行基本命令测试
	details := []string{"✓ SSH连接成功"}

	// 测试基本命令
	if result, err := client.ExecuteCommand("whoami"); err == nil {
		details = append(details, fmt.Sprintf("✓ 当前用户: %s", result.Stdout))
	}

	if result, err := client.ExecuteCommand("uname -a"); err == nil {
		details = append(details, fmt.Sprintf("✓ 系统信息: %s", result.Stdout))
	}

	if result, err := client.ExecuteCommand("free -m"); err == nil {
		details = append(details, fmt.Sprintf("✓ 内存信息: %s", result.Stdout))
	}

	s.logger.Infof("SSH connection successful for %s", req.IP)
	return &model.SSHTestResponse{
		Success: true,
		Details: details,
	}
}

func (s *SSHService) BatchTestConnection(req *model.BatchSSHTestRequest) []*model.SSHTestResponse {
	s.logger.SSHConnectionAttempt("batch", fmt.Sprintf("%d nodes", len(req.Nodes)))

	results := make([]*model.SSHTestResponse, len(req.Nodes))
	var wg sync.WaitGroup

	for i, node := range req.Nodes {
		wg.Add(1)
		go func(index int, n model.BatchNodeRequest) {
			defer wg.Done()

			testReq := &model.SSHTestRequest{
				IP:         n.IP,
				Port:       n.Port,
				Username:   n.Username,
				AuthType:   n.AuthType,
				Password:   n.Password,
				PrivateKey: n.PrivateKey,
				Passphrase: n.Passphrase,
			}

			result := s.TestConnection(testReq)
			result.ID = n.ID
			results[index] = result
		}(i, node)
	}

	wg.Wait()
	return results
}
