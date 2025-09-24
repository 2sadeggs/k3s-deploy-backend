package k3s

import (
	"fmt"
	"strings"
	"time"

	"k3s-deploy-backend/internal/pkg/logger"
	"k3s-deploy-backend/internal/pkg/ssh"
)

type Manager struct {
	logger *logger.Logger
}

func NewManager(logger *logger.Logger) *Manager {
	return &Manager{
		logger: logger,
	}
}

func (m *Manager) GetNodeToken(client *ssh.Client) (string, error) {
	m.logger.Info("获取K3s节点token")

	result, err := client.ExecuteCommand("cat /var/lib/rancher/k3s/server/node-token")
	if err != nil {
		return "", fmt.Errorf("获取节点token失败: %v", err)
	}

	token := strings.TrimSpace(result.Stdout)
	if token == "" {
		return "", fmt.Errorf("节点token为空")
	}

	m.logger.Info("成功获取节点token")
	return token, nil
}

func (m *Manager) ApplyNodeLabels(client *ssh.Client, labels map[string][]string) error {
	m.logger.Info("开始应用节点标签")

	for nodeName, nodeLabels := range labels {
		for _, label := range nodeLabels {
			cmd := fmt.Sprintf("kubectl label nodes %s %s --overwrite", nodeName, label)
			_, err := client.ExecuteCommand(cmd)
			if err != nil {
				m.logger.Errorf("应用标签失败 %s: %v", label, err)
				return fmt.Errorf("为节点 %s 应用标签 %s 失败: %v", nodeName, label, err)
			}
			m.logger.Infof("成功应用标签: %s -> %s", nodeName, label)
		}
	}

	// 验证标签应用
	result, err := client.ExecuteCommand("kubectl get nodes --show-labels")
	if err != nil {
		return fmt.Errorf("验证节点标签失败: %v", err)
	}

	m.logger.Infof("节点标签应用完成:\n%s", result.Stdout)
	return nil
}

func (m *Manager) DeployInSuite(client *ssh.Client, roleAssignment map[string]string) error {
	m.logger.Info("开始部署inSuite应用")

	// 创建命名空间
	if err := m.createNamespace(client); err != nil {
		return err
	}

	// 部署应用组件
	if err := m.deployAppComponents(client, roleAssignment); err != nil {
		return err
	}

	// 等待部署完成
	if err := m.waitForDeployment(client); err != nil {
		return err
	}

	m.logger.Info("inSuite应用部署完成")
	return nil
}

func (m *Manager) createNamespace(client *ssh.Client) error {
	namespaceYaml := `
apiVersion: v1
kind: Namespace
metadata:
  name: insuite
  labels:
    name: insuite
`

	if err := client.UploadFile(namespaceYaml, "/tmp/insuite-namespace.yaml"); err != nil {
		return fmt.Errorf("上传命名空间配置失败: %v", err)
	}

	if _, err := client.ExecuteCommand("kubectl apply -f /tmp/insuite-namespace.yaml"); err != nil {
		return fmt.Errorf("创建命名空间失败: %v", err)
	}

	m.logger.Info("成功创建insuite命名空间")
	return nil
}

func (m *Manager) deployAppComponents(client *ssh.Client, roleAssignment map[string]string) error {
	// 部署数据库组件
	databaseYaml := fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: insuite-database
  namespace: insuite
spec:
  replicas: 1
  selector:
    matchLabels:
      app: insuite-database
  template:
    metadata:
      labels:
        app: insuite-database
    spec:
      nodeSelector:
        insuite.database: "true"
      containers:
      - name: database
        image: postgres:13
        env:
        - name: POSTGRES_DB
          value: "insuite"
        - name: POSTGRES_USER
          value: "insuite"
        - name: POSTGRES_PASSWORD
          value: "insuite123"
        ports:
        - containerPort: 5432
---
apiVersion: v1
kind: Service
metadata:
  name: insuite-database
  namespace: insuite
spec:
  selector:
    app: insuite-database
  ports:
  - port: 5432
    targetPort: 5432
`)

	if err := client.UploadFile(databaseYaml, "/tmp/insuite-database.yaml"); err != nil {
		return fmt.Errorf("上传数据库配置失败: %v", err)
	}

	if _, err := client.ExecuteCommand("kubectl apply -f /tmp/insuite-database.yaml"); err != nil {
		return fmt.Errorf("部署数据库组件失败: %v", err)
	}

	// 部署中间件组件
	middlewareYaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: insuite-middleware
  namespace: insuite
spec:
  replicas: 1
  selector:
    matchLabels:
      app: insuite-middleware
  template:
    metadata:
      labels:
        app: insuite-middleware
    spec:
      nodeSelector:
        insuite.middleware: "true"
      containers:
      - name: middleware
        image: redis:6
        ports:
        - containerPort: 6379
---
apiVersion: v1
kind: Service
metadata:
  name: insuite-middleware
  namespace: insuite
spec:
  selector:
    app: insuite-middleware
  ports:
  - port: 6379
    targetPort: 6379
`

	if err := client.UploadFile(middlewareYaml, "/tmp/insuite-middleware.yaml"); err != nil {
		return fmt.Errorf("上传中间件配置失败: %v", err)
	}

	if _, err := client.ExecuteCommand("kubectl apply -f /tmp/insuite-middleware.yaml"); err != nil {
		return fmt.Errorf("部署中间件组件失败: %v", err)
	}

	// 部署应用组件
	appYaml := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: insuite-app
  namespace: insuite
spec:
  replicas: 1
  selector:
    matchLabels:
      app: insuite-app
  template:
    metadata:
      labels:
        app: insuite-app
    spec:
      nodeSelector:
        insuite.app: "true"
      containers:
      - name: app
        image: nginx:latest
        ports:
        - containerPort: 80
        env:
        - name: DATABASE_URL
          value: "postgres://insuite:insuite123@insuite-database:5432/insuite"
        - name: REDIS_URL
          value: "redis://insuite-middleware:6379"
---
apiVersion: v1
kind: Service
metadata:
  name: insuite-app
  namespace: insuite
spec:
  selector:
    app: insuite-app
  ports:
  - port: 80
    targetPort: 80
  type: NodePort
`

	if err := client.UploadFile(appYaml, "/tmp/insuite-app.yaml"); err != nil {
		return fmt.Errorf("上传应用配置失败: %v", err)
	}

	if _, err := client.ExecuteCommand("kubectl apply -f /tmp/insuite-app.yaml"); err != nil {
		return fmt.Errorf("部署应用组件失败: %v", err)
	}

	return nil
}

func (m *Manager) waitForDeployment(client *ssh.Client) error {
	m.logger.Info("等待所有组件启动...")

	deployments := []string{"insuite-database", "insuite-middleware", "insuite-app"}

	for _, deployment := range deployments {
		for i := 0; i < 30; i++ { // 最多等待5分钟
			result, err := client.ExecuteCommand(fmt.Sprintf("kubectl get deployment %s -n insuite -o jsonpath='{.status.readyReplicas}'", deployment))
			if err == nil && strings.TrimSpace(result.Stdout) == "1" {
				m.logger.Infof("组件 %s 启动成功", deployment)
				break
			}

			if i == 29 {
				return fmt.Errorf("等待组件 %s 启动超时", deployment)
			}

			time.Sleep(10 * time.Second)
		}
	}

	return nil
}

func (m *Manager) VerifyDeployment(client *ssh.Client) error {
	m.logger.Info("开始验证部署状态")

	// 检查所有节点状态
	result, err := client.ExecuteCommand("kubectl get nodes")
	if err != nil {
		return fmt.Errorf("获取节点状态失败: %v", err)
	}
	m.logger.Infof("集群节点状态:\n%s", result.Stdout)

	// 检查Pod状态
	result, err = client.ExecuteCommand("kubectl get pods -n insuite")
	if err != nil {
		return fmt.Errorf("获取Pod状态失败: %v", err)
	}
	m.logger.Infof("inSuite应用状态:\n%s", result.Stdout)

	// 检查服务状态
	result, err = client.ExecuteCommand("kubectl get services -n insuite")
	if err != nil {
		return fmt.Errorf("获取服务状态失败: %v", err)
	}
	m.logger.Infof("inSuite服务状态:\n%s", result.Stdout)

	// 验证所有Pod都在Running状态
	result, err = client.ExecuteCommand("kubectl get pods -n insuite --field-selector=status.phase!=Running --no-headers")
	if err != nil {
		return fmt.Errorf("验证Pod状态失败: %v", err)
	}

	if strings.TrimSpace(result.Stdout) != "" {
		return fmt.Errorf("存在非Running状态的Pod:\n%s", result.Stdout)
	}

	// 获取访问信息
	result, err = client.ExecuteCommand("kubectl get service insuite-app -n insuite -o jsonpath='{.spec.ports[0].nodePort}'")
	if err == nil && result.Stdout != "" {
		m.logger.Infof("inSuite应用访问端口: %s", result.Stdout)
	}

	m.logger.Info("部署验证完成，所有组件运行正常")
	return nil
}
