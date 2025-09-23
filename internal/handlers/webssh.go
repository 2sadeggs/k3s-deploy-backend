package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"k3s-deploy-backend/internal/models"
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
	UsePTY  bool // 标记是否使用 PTY
}

func WebSSHHandler(c *gin.Context) {
	nodeId := c.Query("nodeId")
	if nodeId == "" {
		zap.L().Error("Missing nodeId")
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Missing nodeId"})
		return
	}

	nodesMu.Lock()
	node, exists := nodesMap[nodeId]
	nodesMu.Unlock()
	if !exists {
		zap.L().Error("Node not found", zap.String("nodeId", nodeId))
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Node not found"})
		return
	}

	config := &ssh.ClientConfig{
		User:            node.Username,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
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
			zap.L().Error("Failed to parse private key", zap.Error(err), zap.String("nodeId", nodeId))
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "Invalid private key"})
			return
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	addr := fmt.Sprintf("%s:%d", node.IP, node.Port)
	zap.L().Info("Attempting SSH dial", zap.String("address", addr), zap.String("nodeId", nodeId))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		zap.L().Error("SSH dial failed", zap.String("addr", addr), zap.Error(err), zap.String("nodeId", nodeId))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		zap.L().Error("Failed to create SSH session", zap.Error(err), zap.String("nodeId", nodeId))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		session.Close()
		client.Close()
		zap.L().Error("WebSocket upgrade failed", zap.Error(err), zap.String("nodeId", nodeId))
		return
	}

	sshMu.Lock()
	sshSessions[nodeId] = &SSHSession{Client: client, Session: session, UsePTY: false}
	sshMu.Unlock()

	stdin, err := session.StdinPipe()
	if err != nil {
		ws.Close()
		session.Close()
		client.Close()
		zap.L().Error("Failed to get SSH stdin", zap.Error(err), zap.String("nodeId", nodeId))
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		ws.Close()
		session.Close()
		client.Close()
		zap.L().Error("Failed to get SSH stdout", zap.Error(err), zap.String("nodeId", nodeId))
		return
	}

	ptySuccess := false
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.IGNCR:         1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	var attempt int
	const maxAttempts = 3
	for attempt = 0; attempt < maxAttempts; attempt++ {
		err = session.RequestPty("vt100", 80, 24, modes)
		if err == nil {
			ptySuccess = true
			break
		}
		zap.L().Warn("Failed to request pty, retrying", zap.Error(err), zap.Int("attempt", attempt+1), zap.String("nodeId", nodeId))
		time.Sleep(time.Second * time.Duration(attempt+1))
	}
	if err != nil {
		zap.L().Warn("PTY request failed after retries, falling back to non-interactive mode", zap.Error(err), zap.Int("attempts", maxAttempts), zap.String("nodeId", nodeId))
	} else {
		if err := session.Shell(); err != nil {
			ws.Close()
			session.Close()
			client.Close()
			zap.L().Error("Failed to start SSH shell", zap.Error(err), zap.String("nodeId", nodeId))
			return
		}
		zap.L().Info("Interactive SSH shell started with PTY", zap.String("nodeId", nodeId))
		sshMu.Lock()
		sshSessions[nodeId].UsePTY = true
		sshMu.Unlock()
	}

	// 接收前端命令
	go func() {
		defer func() {
			ws.Close()
			session.Close()
			client.Close()
			zap.L().Info("WebSSH session closed", zap.String("nodeId", nodeId))
		}()
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				zap.L().Warn("WebSocket read error", zap.Error(err), zap.String("nodeId", nodeId))
				return
			}
			var data struct{ Command string }
			if err := json.Unmarshal(msg, &data); err != nil {
				zap.L().Error("Invalid WebSocket message", zap.Error(err), zap.String("nodeId", nodeId))
				continue
			}
			zap.L().Info("Received command from frontend", zap.String("command", data.Command), zap.String("nodeId", nodeId))
			if ptySuccess {
				n, err := stdin.Write([]byte(data.Command))
				if err != nil {
					zap.L().Error("Failed to write to SSH stdin", zap.Error(err), zap.Int("bytesWritten", n), zap.String("command", data.Command), zap.String("nodeId", nodeId))
					return
				}
				zap.L().Info("Wrote to SSH stdin", zap.Int("bytesWritten", n), zap.String("command", data.Command), zap.String("nodeId", nodeId))
			} else {
				cmdSession, err := client.NewSession()
				if err != nil {
					zap.L().Error("Failed to create command session", zap.Error(err), zap.String("nodeId", nodeId))
					continue
				}
				output, err := cmdSession.CombinedOutput(data.Command)
				cmdSession.Close()
				if err != nil {
					zap.L().Error("Command execution failed", zap.Error(err), zap.String("command", data.Command), zap.String("nodeId", nodeId))
					ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v\n", err)))
					continue
				}
				ws.WriteMessage(websocket.TextMessage, output)
				zap.L().Info("Command executed in non-interactive mode", zap.String("command", data.Command), zap.String("output", string(output)), zap.String("nodeId", nodeId))
			}
		}
	}()

	// 发送 SSH 输出（仅交互模式）
	if ptySuccess {
		go func() {
			defer func() {
				ws.Close()
				session.Close()
				client.Close()
				zap.L().Info("WebSSH output goroutine closed", zap.String("nodeId", nodeId))
			}()
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				data := scanner.Bytes()
				if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
					zap.L().Warn("WebSocket write error", zap.Error(err), zap.String("nodeId", nodeId))
					return
				}
				zap.L().Info("Sent output to frontend", zap.String("output", string(data)), zap.String("nodeId", nodeId))
			}
			if err := scanner.Err(); err != nil {
				zap.L().Error("Scanner error", zap.Error(err), zap.String("nodeId", nodeId))
			}
		}()
	}

	ws.SetCloseHandler(func(code int, text string) error {
		sshMu.Lock()
		delete(sshSessions, nodeId)
		sshMu.Unlock()
		session.Close()
		client.Close()
		zap.L().Info("WebSSH session closed by client", zap.Int("code", code), zap.String("reason", text), zap.String("nodeId", nodeId))
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
