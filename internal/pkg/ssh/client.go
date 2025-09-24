package ssh

import (
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHConfig struct {
	Host       string
	Port       int
	Username   string
	AuthType   string
	Password   string
	PrivateKey string
	Passphrase string
}

type Client struct {
	config SSHConfig
	conn   *ssh.Client
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func NewClient(config SSHConfig) *Client {
	return &Client{
		config: config,
	}
}

func (c *Client) Connect() error {
	var auth []ssh.AuthMethod

	if c.config.AuthType == "password" {
		auth = append(auth, ssh.Password(c.config.Password))
	} else if c.config.AuthType == "key" {
		signer, err := c.parsePrivateKey(c.config.PrivateKey, c.config.Passphrase)
		if err != nil {
			return fmt.Errorf("解析私钥失败: %v", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}

	config := &ssh.ClientConfig{
		User:            c.config.Username,
		Auth:            auth,
		Timeout:         30 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 注意：生产环境应该验证主机密钥
	}

	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("SSH连接失败: %v", err)
	}

	c.conn = conn
	return nil
}

func (c *Client) parsePrivateKey(privateKey, passphrase string) (ssh.Signer, error) {
	var signer ssh.Signer
	var err error

	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(privateKey))
	}

	if err != nil {
		return nil, err
	}

	return signer, nil
}

func (c *Client) ExecuteCommand(cmd string) (*CommandResult, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("SSH连接未建立")
	}

	session, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("创建SSH会话失败: %v", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf strings.Builder
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	err = session.Run(cmd)

	result := &CommandResult{
		Stdout: strings.TrimSpace(stdoutBuf.String()),
		Stderr: strings.TrimSpace(stderrBuf.String()),
	}

	if err != nil {
		if exitError, ok := err.(*ssh.ExitError); ok {
			result.ExitCode = exitError.ExitStatus()
		} else {
			result.ExitCode = 1
		}
		return result, fmt.Errorf("命令执行失败: %v", err)
	}

	result.ExitCode = 0
	return result, nil
}

func (c *Client) UploadFile(content, remotePath string) error {
	if c.conn == nil {
		return fmt.Errorf("SSH连接未建立")
	}

	session, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("创建SSH会话失败: %v", err)
	}
	defer session.Close()

	w, err := session.StdinPipe()
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("cat > %s", remotePath)
	if err := session.Start(cmd); err != nil {
		return err
	}

	_, err = io.WriteString(w, content)
	if err != nil {
		return err
	}
	w.Close()

	return session.Wait()
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) IsPortOpen(port int) bool {
	addr := fmt.Sprintf("%s:%d", c.config.Host, port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
