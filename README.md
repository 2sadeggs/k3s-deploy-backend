# K3s Deploy Backend

一个用于自动化部署K3s集群并安装inSuite应用的后端服务。

## 功能特性

- 🚀 **自动化K3s集群部署**：支持单节点、双节点、三节点部署模式
- 🔐 **多种SSH认证**：支持密码和密钥认证方式
- 📊 **实时部署监控**：提供详细的部署进度和日志
- 🏷️ **智能节点标签**：自动为节点分配角色标签
- 📦 **应用自动部署**：自动部署inSuite应用组件
- 🔍 **部署验证**：自动验证集群和应用部署状态

## 系统架构

```
├── cmd/server/           # 应用入口
├── internal/
│   ├── handler/          # HTTP处理层
│   ├── service/          # 业务逻辑层
│   ├── model/           # 数据模型
│   ├── pkg/             # 核心组件
│   │   ├── ssh/         # SSH客户端
│   │   ├── k3s/         # K3s管理
│   │   └── logger/      # 日志组件
│   └── router/          # 路由配置
├── pkg/utils/           # 工具函数
└── scripts/             # 安装脚本
```

## 快速开始

### 环境要求

- Go 1.19+
- 目标节点需要root权限
- 目标节点需要支持curl和systemd

### 安装依赖

```bash
go mod tidy
```

### 启动服务

```bash
go run cmd/server/main.go
```

服务将在 `http://localhost:8080` 启动

## API 接口

### SSH连接测试

**单节点测试**
```bash
POST /api/ssh/test
{
  "ip": "192.168.1.100",
  "port": 22,
  "username": "root",
  "authType": "password",
  "password": "your_password"
}
```

**批量测试**
```bash
POST /api/ssh/test-batch
{
  "nodes": [
    {
      "id": 1,
      "name": "k3s-master",
      "ip": "192.168.1.100",
      "port": 22,
      "username": "root",
      "authType": "password",
      "password": "your_password"
    }
  ]
}
```

### K3s集群部署

```bash
POST /api/k3s/deploy
{
  "deployMode": "single",
  "step": "validate",
  "nodes": [...],
  "roleAssignment": {
    "app": "k3s-master",
    "middleware": "k3s-master",
    "database": "k3s-master"
  },
  "labels": {...}
}
```

## 部署步骤

1. **validate** - 验证节点连接和系统要求
2. **install-master** - 安装K3s Master节点
3. **configure-agent** - 配置K3s Agent节点
4. **apply-labels** - 应用节点标签
5. **deploy-insuite** - 部署inSuite应用
6. **verify** - 验证部署状态

## 配置说明

### 环境变量

```bash
# 服务器配置
SERVER_PORT=8080
READ_TIMEOUT=30
WRITE_TIMEOUT=30

# K3s配置
K3S_VERSION=latest
DEFAULT_NAMESPACE=insuite

# SSH配置
SSH_CONNECT_TIMEOUT=30
SSH_COMMAND_TIMEOUT=300
SSH_MAX_RETRIES=3

# 日志配置
LOG_LEVEL=info
LOG_FORMAT=text
```

### 组件镜像

- **数据库**: postgres:13
- **中间件**: redis:6
- **应用**: nginx:latest

## 安全注意事项

1. **SSH连接**: 生产环境建议使用密钥认证
2. **主机密钥验证**: 当前为开发模式，生产环境需要验证主机密钥
3. **网络安全**: 确保K3s API端口(6443)的网络安全
4. **权限管理**: 部署用户需要具有root权限

## 故障排除

### 常见问题

1. **SSH连接失败**
    - 检查IP地址和端口
    - 验证认证信息
    - 确认目标主机SSH服务运行状态

2. **K3s安装失败**
    - 检查网络连接
    - 验证系统要求（内存、磁盘）
    - 查看安装日志

3. **应用部署失败**
    - 检查节点标签配置
    - 验证镜像拉取状态
    - 查看Pod日志

### 日志查看

服务日志包含详细的操作信息：
```bash
# 查看实时日志
tail -f /var/log/k3s-deploy.log

# 查看SSH连接日志
grep "ssh_connection" /var/log/k3s-deploy.log

# 查看部署进度
grep "deployment" /var/log/k3s-deploy.log
```

## 开发指南

### 添加新的部署步骤

1. 在 `internal/service/deploy_service.go` 中添加步骤处理函数
2. 在 `stepHandlers` 映射中注册新步骤
3. 更新前端的步骤配置

### 自定义组件镜像

修改 `internal/pkg/k3s/manager.go` 中的部署配置，或通过环境变量覆盖默认镜像。

## 许可证

MIT License 