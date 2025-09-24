package services

import (
	"fmt"
	"k3s-deploy-backend/internal/models" // 添加导入
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type SSHService struct {
	logger *zap.Logger
}

func NewSSHService() *SSHService {
	return &SSHService{logger: zap.L()}
}

func (s *SSHService) TestConnection(req *models.SSHTestRequestWithID) (success bool, details []string, err error) {
	config := &ssh.ClientConfig{
		User:            req.Username,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 开发环境
		Timeout:         10 * time.Second,
	}

	if req.AuthType == "password" {
		config.Auth = append(config.Auth, ssh.Password(req.Password))
	} else if req.AuthType == "key" {
		var signer ssh.Signer
		if req.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(req.PrivateKey), []byte(req.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(req.PrivateKey))
		}
		if err != nil {
			s.logger.Error("Failed to parse private key", zap.Error(err))
			return false, nil, fmt.Errorf("parse private key: %v", err)
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	addr := fmt.Sprintf("%s:%d", req.IP, req.Port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		s.logger.Error("SSH dial failed", zap.String("addr", addr), zap.Error(err))
		return false, []string{err.Error()}, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		s.logger.Error("Failed to create SSH session", zap.Error(err))
		return false, []string{err.Error()}, err
	}
	defer session.Close()

	output, err := session.CombinedOutput("uname -a && df -h")
	if err != nil {
		s.logger.Error("Failed to execute command", zap.Error(err))
		return false, []string{err.Error()}, err
	}

	details = strings.Split(strings.TrimSpace(string(output)), "\n")
	s.logger.Info("SSH test successful", zap.String("node", req.Name))
	return true, details, nil
}

func (s *SSHService) ExecuteCommand(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
