package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"k3s-deploy-backend/internal/models"
	"k3s-deploy-backend/internal/services"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == "http://localhost:3000"
	},
}

var (
	sshSessions = make(map[string]*SSHSession)
	sshMu       sync.Mutex
)

type SSHSession struct {
	Client  *ssh.Client
	Session *ssh.Session
}

func WebSSHHandler(c *gin.Context) {
	nodeId := c.Query("nodeId")
	if nodeId == "" {
		zap.L().Error("Missing nodeId")
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Missing nodeId"})
		return
	}

	// 从内存 map 获取节点信息
	node := getNodeById(nodeId)
	if node == nil {
		zap.L().Error("Node not found", zap.String("nodeId", nodeId))
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Node not found"})
		return
	}

	// 建立 SSH 连接
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
			zap.L().Error("Failed to parse private key", zap.Error(err))
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "Invalid private key"})
			return
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	addr := fmt.Sprintf("%s:%d", node.IP, node.Port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		zap.L().Error("SSH dial failed", zap.String("addr", addr), zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	sshService := services.NewSSHService()
	session, err := sshService.CreateInteractiveSession(client)
	if err != nil {
		client.Close()
		zap.L().Error("Failed to create SSH session", zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		session.Close()
		client.Close()
		zap.L().Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	sshMu.Lock()
	sshSessions[nodeId] = &SSHSession{Client: client, Session: session}
	sshMu.Unlock()

	stdin, err := session.StdinPipe()
	if err != nil {
		ws.Close()
		session.Close()
		client.Close()
		zap.L().Error("Failed to get SSH stdin", zap.Error(err))
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		ws.Close()
		session.Close()
		client.Close()
		zap.L().Error("Failed to get SSH stdout", zap.Error(err))
		return
	}

	if err := session.Shell(); err != nil {
		ws.Close()
		session.Close()
		client.Close()
		zap.L().Error("Failed to start SSH shell", zap.Error(err))
		return
	}

	// 接收前端命令
	go func() {
		defer ws.Close()
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				zap.L().Warn("WebSocket read error", zap.Error(err))
				break
			}
			var data struct{ Command string }
			if err := json.Unmarshal(msg, &data); err != nil {
				zap.L().Error("Invalid WebSocket message", zap.Error(err))
				continue
			}
			if _, err := stdin.Write([]byte(data.Command)); err != nil {
				zap.L().Error("Failed to write to SSH stdin", zap.Error(err))
				break
			}
		}
	}()

	// 发送 SSH 输出
	go func() {
		defer ws.Close()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if err := ws.WriteMessage(websocket.TextMessage, scanner.Bytes()); err != nil {
				zap.L().Warn("WebSocket write error", zap.Error(err))
				break
			}
		}
	}()

	ws.SetCloseHandler(func(code int, text string) error {
		sshMu.Lock()
		delete(sshSessions, nodeId)
		sshMu.Unlock()
		session.Close()
		client.Close()
		zap.L().Info("WebSSH session closed", zap.String("nodeId", nodeId))
		return nil
	})
}

func getNodeById(nodeId string) *models.SSHTestRequestWithID {
	nodesMu.Lock()
	defer nodesMu.Unlock()
	node, exists := nodesMap[nodeId]
	if !exists {
		return nil
	}
	return &node
}
