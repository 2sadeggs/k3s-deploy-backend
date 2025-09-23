package handlers

import (
	"fmt"
	"k3s-deploy-backend/internal/models"
	"k3s-deploy-backend/internal/services"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

var (
	taskProgress = make(map[string]models.ProgressResponse)
	taskMu       sync.Mutex
)

func K3sDeployHandler(c *gin.Context) {
	var req models.DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		zap.L().Error("Invalid deploy request", zap.Error(err))
		c.JSON(http.StatusBadRequest, models.DeployResponse{
			Success: false,
			Message: "Invalid request payload",
		})
		return
	}

	taskId := uuid.New().String()
	taskMu.Lock()
	taskProgress[taskId] = models.ProgressResponse{
		Success:  true,
		Progress: 0,
		Status:   "deploying",
		Logs:     []string{"Deployment started"},
	}
	taskMu.Unlock()

	go func() {
		sshService := services.NewSSHService()
		steps := []struct {
			name     string
			duration time.Duration
			action   func() error
		}{
			{
				name:     "Validate nodes",
				duration: 2 * time.Second,
				action: func() error {
					for _, node := range req.Nodes {
						success, _, err := sshService.TestConnection(&node)
						if !success {
							return fmt.Errorf("node %s validation failed: %v", node.Name, err)
						}
					}
					return nil
				},
			},
			{
				name:     "Install K3s Master",
				duration: 8 * time.Second,
				action: func() error {
					for _, node := range req.Nodes {
						if node.Name == "k3s-master" {
							client, err := createSSHClient(&node)
							if err != nil {
								return err
							}
							defer client.Close()
							_, err = sshService.ExecuteCommand(client, "curl -sfL https://get.k3s.io | sh -")
							return err
						}
					}
					return nil
				},
			},
			{
				name:     "Configure K3s Agents",
				duration: 6 * time.Second,
				action: func() error {
					// 假设获取master token，实际需从master节点提取
					token := "your_k3s_token" // 替换为实际token
					for _, node := range req.Nodes {
						if node.Name != "k3s-master" {
							client, err := createSSHClient(&node)
							if err != nil {
								return err
							}
							defer client.Close()
							cmd := fmt.Sprintf("curl -sfL https://get.k3s.io | K3S_URL=https://%s:6443 K3S_TOKEN=%s sh -", req.Nodes[0].IP, token)
							_, err = sshService.ExecuteCommand(client, cmd)
							if err != nil {
								return err
							}
						}
					}
					return nil
				},
			},
			{
				name:     "Apply node labels",
				duration: 3 * time.Second,
				action: func() error {
					client, err := createSSHClient(&req.Nodes[0])
					if err != nil {
						return err
					}
					defer client.Close()
					for nodeName, labels := range req.Labels {
						for _, label := range labels {
							cmd := fmt.Sprintf("kubectl label nodes %s %s", nodeName, label)
							_, err := sshService.ExecuteCommand(client, cmd)
							if err != nil {
								return err
							}
						}
					}
					return nil
				},
			},
			{
				name:     "Deploy inSuite",
				duration: 10 * time.Second,
				action: func() error {
					client, err := createSSHClient(&req.Nodes[0])
					if err != nil {
						return err
					}
					defer client.Close()
					// 假设inSuite.yaml已存在
					_, err = sshService.ExecuteCommand(client, "kubectl apply -f insuite.yaml")
					return err
				},
			},
		}

		for i, step := range steps {
			taskMu.Lock()
			progress := taskProgress[taskId]
			progress.Logs = append(progress.Logs, fmt.Sprintf("Starting %s", step.name))
			taskProgress[taskId] = progress
			taskMu.Unlock()

			err := step.action()
			taskMu.Lock()
			progress = taskProgress[taskId]
			progress.Progress = float64((i+1)*100) / float64(len(steps))
			if err != nil {
				progress.Status = "error"
				progress.Error = err.Error()
				progress.Logs = append(progress.Logs, fmt.Sprintf("Failed %s: %v", step.name, err))
				taskProgress[taskId] = progress
				taskMu.Unlock()
				zap.L().Error("Deployment step failed", zap.String("step", step.name), zap.Error(err))
				return
			}
			progress.Logs = append(progress.Logs, fmt.Sprintf("Completed %s", step.name))
			taskProgress[taskId] = progress
			taskMu.Unlock()

			time.Sleep(step.duration) // 模拟耗时
		}

		taskMu.Lock()
		progress := taskProgress[taskId]
		progress.Status = "success"
		progress.Logs = append(progress.Logs, "Deployment completed successfully")
		taskProgress[taskId] = progress
		taskMu.Unlock()
	}()

	c.JSON(http.StatusOK, models.DeployResponse{
		Success: true,
		TaskID:  taskId,
		Message: "Deployment started",
	})
}

func K3sProgressHandler(c *gin.Context) {
	taskId := c.Param("taskId")
	taskMu.Lock()
	progress, exists := taskProgress[taskId]
	taskMu.Unlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Task not found"})
		return
	}

	c.JSON(http.StatusOK, progress)
}

func createSSHClient(node *models.SSHTestRequestWithID) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            node.Username,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	if node.AuthType == "password" {
		config.Auth = append(config.Auth, ssh.Password(node.Password))
	} else if node.AuthType == "key" {
		var signer ssh.Signer
		var err error
		if node.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(node.PrivateKey), []byte(node.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(node.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %v", err)
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	addr := fmt.Sprintf("%s:%d", node.IP, node.Port)
	return ssh.Dial("tcp", addr, config)
}
