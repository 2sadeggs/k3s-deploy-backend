#!/bin/bash

# K3s Master 安装脚本
set -e

echo "开始安装 K3s Master..."

# 检查系统要求
check_system() {
echo "检查系统要求..."

# 检查操作系统
if [ -f /etc/os-release ]; then
. /etc/os-release
echo "操作系统: $NAME $VERSION"
fi

# 检查内存
MEMORY_MB=$(free -m | awk 'NR==2{printf "%.0f", $2}')
echo "可用内存: ${MEMORY_MB}MB"

if [ "$MEMORY_MB" -lt 512 ]; then
echo "警告: 内存少于512MB，可能影响K3s性能"
fi

# 检查磁盘空间
DISK_GB=$(df -BG / | awk 'NR==2{print $4}' | sed 's/G//')
echo "可用磁盘空间: ${DISK_GB}GB"

if [ "$DISK_GB" -lt 2 ]; then
echo "警告: 磁盘空间少于2GB，可能不足以运行K3s"
fi
}

# 安装K3s
install_k3s() {
echo "下载并安装 K3s..."

# 设置安装参数
export INSTALL_K3S_EXEC="--disable traefik --write-kubeconfig-mode 644"

# 下载并执行安装脚本
curl -sfL https://get.k3s.io | sh -

if [ $? -ne 0 ]; then
echo "错误: K3s 安装失败"
exit 1
fi
}

# 等待K3s启动
wait_for_k3s() {
echo "等待 K3s 启动..."

for i in {1..30}; do
if systemctl is-active --quiet k3s; then
echo "K3s 服务已启动"
break
fi

if [ $i -eq 30 ]; then
echo "错误: K3s 启动超时"
systemctl status k3s --no-pager -l
exit 1
fi

echo "等待中... ($i/30)"
sleep 2
done

# 等待API服务器可用
echo "等待 API 服务器可用..."
for i in {1..30}; do
if kubectl get nodes > /dev/null 2>&1; then
echo "API 服务器已就绪"
break
fi

if [ $i -eq 30 ]; then
echo "错误: API 服务器启动超时"
exit 1
fi

echo "等待 API 服务器... ($i/30)"
sleep 2
done
}

# 验证安装
verify_installation() {
echo "验证 K3s 安装..."

# 检查服务状态
if ! systemctl is-active --quiet k3s; then
echo "错误: K3s 服务未运行"
systemctl status k3s --no-pager -l
exit 1
fi

echo "✓ K3s 服务正在运行"

# 检查节点状态
NODE_STATUS=$(kubectl get nodes --no-headers | awk '{print $2}')
if [ "$NODE_STATUS" != "Ready" ]; then
echo "错误: 节点状态异常: $NODE_STATUS"
kubectl get nodes
exit 1
fi

echo "✓ 节点状态正常"

# 显示安装信息
echo ""
echo "=== K3s Master 安装完成 ==="
echo "节点信息:"
kubectl get nodes -o wide

echo ""
echo "系统Pod:"
kubectl get pods -n kube-system

echo ""
echo "Token位置: /var/lib/rancher/k3s/server/node-token"
echo "Kubeconfig: /etc/rancher/k3s/k3s.yaml"
}

# 主函数
main() {
echo "========================================="
echo "        K3s Master 自动安装脚本"
echo "========================================="

check_system
install_k3s
wait_for_k3s
verify_installation

echo ""
echo "🎉 K3s Master 安装成功！"
echo "现在可以添加 Agent 节点到集群中"
}

# 错误处理
trap 'echo "错误: 安装过程中发生异常"; exit 1' ERR

# 执行主函数
main