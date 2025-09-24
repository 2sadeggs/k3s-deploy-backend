#!/bin/bash

# K3s Agent 安装脚本
set -e

# 检查参数
if [ $# -ne 2 ]; then
    echo "用法: $0 <master_ip> <token>"
    echo "示例: $0 192.168.1.100 K10abc123::server:xyz789"
    exit 1
fi

MASTER_IP="$1"
TOKEN="$2"

echo "开始安装 K3s Agent..."
echo "Master IP: $MASTER_IP"
echo "Token: ${TOKEN:0:10}..."

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

    if [ "$MEMORY_MB" -lt 256 ]; then
        echo "警告: 内存少于256MB，可能影响Agent性能"
    fi

    # 检查网络连通性
    echo "检查与Master节点的连通性..."
    if ! ping -c 3 "$MASTER_IP" > /dev/null 2>&1; then
        echo "警告: 无法ping通Master节点 $MASTER_IP"
    else
        echo "✓ 网络连通性正常"
    fi

    # 检查Master API端口
    if command -v telnet > /dev/null 2>&1; then
        if echo "" | telnet "$MASTER_IP" 6443 2>/dev/null | grep -q "Connected"; then
            echo "✓ Master API端口 6443 可访问"
        else
            echo "警告: 无法连接到Master API端口 6443"
        fi
    fi
}

# 安装K3s Agent
install_k3s_agent() {
    echo "下载并安装 K3s Agent..."

    # 设置环境变量
    export K3S_URL="https://$MASTER_IP:6443"
    export K3S_TOKEN="$TOKEN"

    # 下载并执行安装脚本
    curl -sfL https://get.k3s.io | sh -

    if [ $? -ne 0 ]; then
        echo "错误: K3s Agent 安装失败"
        exit 1
    fi
}

# 等待Agent启动
wait_for_agent() {
    echo "等待 K3s Agent 启动..."

    for i in {1..30}; do
        if systemctl is-active --quiet k3s-agent; then
            echo "K3s Agent 服务已启动"
            break
        fi

        if [ $i -eq 30 ]; then
            echo "错误: K3s Agent 启动超时"
            systemctl status k3s-agent --no-pager -l
            exit 1
        fi

        echo "等待中... ($i/30)"
        sleep 2
    done
}

# 验证安装
verify_installation() {
    echo "验证 K3s Agent 安装..."

    # 检查服务状态
    if ! systemctl is-active --quiet k3s-agent; then
        echo "错误: K3s Agent 服务未运行"
        systemctl status k3s-agent --no-pager -l
        exit 1
    fi

    echo "✓ K3s Agent 服务正在运行"

    # 检查日志中是否有错误
    echo "检查服务日志..."
    if journalctl -u k3s-agent --no-pager -l -n 20 | grep -i error; then
        echo "警告: 发现错误日志，请检查"
    else
        echo "✓ 服务日志正常"
    fi
}

# 主函数
main() {
    echo "========================================="
    echo "        K3s Agent 自动安装脚本"
    echo "========================================="

    check_system
    install_k3s_agent
    wait_for_agent
    verify_installation

    echo ""
    echo "🎉 K3s Agent 安装成功！"
    echo "Agent 已加入集群，请在Master节点验证:"
    echo "kubectl get nodes"
}

# 错误处理
trap 'echo "错误: 安装过程中发生异常"; exit 1' ERR

# 执行主函数
main