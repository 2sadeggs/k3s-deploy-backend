package k3s

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"k3s-deploy-backend/internal/pkg/logger"
	"k3s-deploy-backend/internal/pkg/ssh"
)

const (
	officialInstallURL       = "https://get.k3s.io"
	officialCNInstallURL     = "https://rancher-mirror.rancher.cn/k3s/k3s-install.sh"
	defaultSystemRegistryURL = "registry.cn-hangzhou.aliyuncs.com"
	additionalRegistryURLs   = "https://registry.cn-hangzhou.aliyuncs.com,https://mirror.ccs.tencentyun.com"
	clientExpirationYears    = 10
	daysInYear               = 365
)

type Installer struct {
	logger *logger.Logger
}

type ModifyOptions struct {
	EnableRegistry        bool
	EnableCertConfig      bool
	ClientExpirationYears int
	DaysInYear            int
}

func NewInstaller(logger *logger.Logger) *Installer {
	return &Installer{
		logger: logger,
	}
}

func (i *Installer) InstallMaster(client *ssh.Client, nodeName string) error {
	i.logger.Infof("开始在节点 %s 上安装K3s Master", nodeName)

	// 检查是否已经安装K3s
	if result, err := client.ExecuteCommand("which k3s"); err == nil && result.Stdout != "" {
		i.logger.Warnf("节点 %s 已经安装了K3s，跳过安装步骤", nodeName)
		return nil
	}

	// 使用高级安装逻辑
	envArgs := []string{
		"INSTALL_K3S_EXEC=--disable traefik --write-kubeconfig-mode 644",
	}
	cmdArgs := []string{}

	if err := i.autoInstallK3sByLocation(client, envArgs, cmdArgs); err != nil {
		return fmt.Errorf("K3s Master安装失败: %v", err)
	}

	// 验证安装
	if err := i.verifyMasterInstallation(client); err != nil {
		return fmt.Errorf("验证Master安装失败: %v", err)
	}

	i.logger.Infof("节点 %s K3s Master安装成功", nodeName)
	return nil
}

func (i *Installer) InstallAgent(client *ssh.Client, nodeName, masterIP, token string) error {
	i.logger.Infof("开始在节点 %s 上安装K3s Agent", nodeName)

	// 检查是否已经安装K3s
	if result, err := client.ExecuteCommand("which k3s"); err == nil && result.Stdout != "" {
		i.logger.Warnf("节点 %s 已经安装了K3s，跳过安装步骤", nodeName)
		return nil
	}

	// Agent环境变量
	envArgs := []string{
		fmt.Sprintf("K3S_URL=https://%s:6443", masterIP),
		fmt.Sprintf("K3S_TOKEN=%s", token),
	}
	cmdArgs := []string{}

	if err := i.autoInstallK3sByLocation(client, envArgs, cmdArgs); err != nil {
		return fmt.Errorf("K3s Agent安装失败: %v", err)
	}

	i.logger.Infof("节点 %s K3s Agent安装成功", nodeName)
	return nil
}

// 自定义k3s安装 -- 根据国内环境国外环境判断使用国内源或官方国外源
func (i *Installer) autoInstallK3sByLocation(client *ssh.Client, envArgs, cmdArgs []string) error {
	// 获取安装脚本URL
	installURL, err := i.getInstallURL(client)
	if err != nil {
		return err
	}

	i.logger.Infof("使用安装URL: %s", installURL)
	// 执行安装
	return i.executeInstall(client, installURL, envArgs, cmdArgs)
}

// 获取安装脚本URL
func (i *Installer) getInstallURL(client *ssh.Client) (string, error) {
	// 判断是否在中国大陆
	if isChina, err := i.isInMainlandChina(client); err != nil {
		i.logger.Warnf("无法判断网络环境，默认使用国内源: %v", err)
		return officialCNInstallURL, nil
	} else if isChina {
		return officialCNInstallURL, nil
	}
	return officialInstallURL, nil
}

// 判断是否是国内环境
func (i *Installer) isInMainlandChina(client *ssh.Client) (bool, error) {
	// 如果无法访问互联网，假定为国内环境
	if reachable, _ := i.isInternetReachable(client, "http://www.baidu.com"); !reachable {
		return true, nil
	}

	// 如果程序能访问到谷歌，结果为true再取反为false表示不是中国大陆
	if reachable, _ := i.isInternetReachable(client, "http://www.google.com"); !reachable {
		return true, nil
	}

	return false, nil
}

// 检测网络可达性
func (i *Installer) isInternetReachable(client *ssh.Client, url string) (bool, error) {
	cmd := fmt.Sprintf("curl -s --connect-timeout 3 --max-time 5 %s > /dev/null 2>&1", url)
	result, err := client.ExecuteCommand(cmd)
	return err == nil && result.ExitCode == 0, err
}

// 执行安装
func (i *Installer) executeInstall(client *ssh.Client, installURL string, envArgs, cmdArgs []string) error {
	i.logger.Infof("=== K3s 安装调试信息 ===")
	i.logger.Infof("安装URL: %s", installURL)
	i.logger.Infof("环境变量数量: %d", len(envArgs))
	i.logger.Infof("命令参数数量: %d", len(cmdArgs))

	// Step 0: 检测操作系统类型
	i.logger.Info("Step 0: 检测操作系统类型")
	isDomestic, osName, err := i.isDomesticOS(client)
	if err != nil {
		i.logger.Warnf("操作系统检测失败: %v", err)
	}

	if isDomestic {
		i.logger.Infof("检测到国产操作系统: %s", osName)
		i.logger.Info("将跳过SELinux配置以提高兼容性")
	} else {
		i.logger.Info("检测到标准Linux发行版")
		i.logger.Info("将使用默认SELinux处理")
	}

	// Step 1: 下载脚本
	i.logger.Info("Step 1: 下载K3s安装脚本")
	resp, err := http.Get(installURL)
	if err != nil {
		return fmt.Errorf("下载安装脚本失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载脚本失败: HTTP %d", resp.StatusCode)
	}

	script, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取脚本内容失败: %v", err)
	}

	i.logger.Infof("脚本下载成功，大小: %d bytes", len(script))

	// Step 2: 修改脚本
	i.logger.Info("Step 2: 修改安装脚本")
	var modifiedScript []byte

	switch installURL {
	case officialInstallURL:
		i.logger.Info("使用官方安装URL - 仅应用证书配置")
		modifiedScript, err = i.modifyScriptSelective(script, ModifyOptions{
			EnableRegistry:        false,
			EnableCertConfig:      true,
			ClientExpirationYears: clientExpirationYears,
			DaysInYear:            daysInYear,
		})
	case officialCNInstallURL:
		i.logger.Info("使用国内镜像URL - 应用注册表设置和证书配置")
		modifiedScript, err = i.modifyScriptSelective(script, ModifyOptions{
			EnableRegistry:        true,
			EnableCertConfig:      true,
			ClientExpirationYears: clientExpirationYears,
			DaysInYear:            daysInYear,
		})
	default:
		i.logger.Infof("使用未知/自定义URL (%s) - 不应用修改", installURL)
		modifiedScript = script
	}

	if err != nil {
		return fmt.Errorf("修改脚本失败: %v", err)
	}

	i.logger.Infof("脚本修改完成，最终大小: %d bytes", len(modifiedScript))

	// Step 3: 保存脚本到远程主机
	scriptPath := "/tmp/k3s-install-modified.sh"
	i.logger.Infof("Step 3: 保存修改后的脚本到 %s", scriptPath)

	if err := client.UploadFile(string(modifiedScript), scriptPath); err != nil {
		return fmt.Errorf("上传安装脚本失败: %v", err)
	}

	// Step 4: 生成自定义CA证书
	i.logger.Info("Step 4: 生成自定义CA证书")
	if err := i.generateCustomCACerts(client); err != nil {
		i.logger.Warnf("生成自定义CA证书失败: %v", err)
	}

	// Step 5: 准备环境变量和参数
	i.logger.Info("Step 5: 准备环境变量和参数")
	finalEnvArgs := make([]string, len(envArgs))
	copy(finalEnvArgs, envArgs)
	finalCmdArgs := make([]string, len(cmdArgs))
	copy(finalCmdArgs, cmdArgs)

	// 如果是国产操作系统，添加SELinux跳过配置
	if isDomestic {
		i.logger.Infof("--- 国产操作系统配置 ---")
		i.logger.Infof("操作系统名称: %s", osName)

		selinuxBypassEnvs := []string{
			"INSTALL_K3S_SELINUX_WARN=true",
			"INSTALL_K3S_SKIP_SELINUX_RPM=true",
		}
		finalEnvArgs = append(finalEnvArgs, selinuxBypassEnvs...)
		i.logger.Info("已添加SELinux绕过配置")
	}

	// 如果是国内源自动添加参数
	if installURL == officialCNInstallURL {
		i.logger.Info("--- 国内镜像配置 ---")

		additionalEnvs := []string{
			"INSTALL_K3S_MIRROR=cn",
			fmt.Sprintf("INSTALL_K3S_REGISTRIES=%s", additionalRegistryURLs),
		}
		finalEnvArgs = append(finalEnvArgs, additionalEnvs...)

		additionalArgs := []string{
			fmt.Sprintf("--system-default-registry=%s", defaultSystemRegistryURL),
			"--disable-default-registry-endpoint",
		}
		finalCmdArgs = append(finalCmdArgs, additionalArgs...)
		i.logger.Info("已添加国内镜像配置")
	}

	// Step 6: 执行安装
	i.logger.Info("Step 6: 开始执行安装")

	// 构建命令
	envStr := strings.Join(finalEnvArgs, " ")
	var cmd string
	if len(finalCmdArgs) > 0 {
		cmd = fmt.Sprintf("cd /tmp && %s bash k3s-install-modified.sh %s", envStr, strings.Join(finalCmdArgs, " "))
	} else {
		cmd = fmt.Sprintf("cd /tmp && %s bash k3s-install-modified.sh", envStr)
	}

	i.logger.Infof("执行命令: %s", cmd)

	// 设置更长的超时时间
	result, err := client.ExecuteCommand(cmd)
	if err != nil {
		i.logger.Errorf("K3s安装失败: %v", err)
		if result != nil {
			i.logger.Errorf("标准输出: %s", result.Stdout)
			i.logger.Errorf("错误输出: %s", result.Stderr)
		}
		return fmt.Errorf("K3s安装失败: %v", err)
	}

	i.logger.Info("K3s安装完成!")
	return nil
}

// 检测是否为国产操作系统
func (i *Installer) isDomesticOS(client *ssh.Client) (bool, string, error) {
	// 读取/etc/os-release文件
	result, err := client.ExecuteCommand("cat /etc/os-release 2>/dev/null || echo 'not_found'")
	if err != nil {
		return i.checkAlternativeOSDetection(client)
	}

	if result.Stdout == "not_found" {
		return i.checkAlternativeOSDetection(client)
	}

	content := strings.ToLower(result.Stdout)

	// 国产操作系统的标识关键词
	domesticOSKeywords := map[string]string{
		"kylin":     "银河麒麟",
		"uos":       "统信UOS",
		"deepin":    "深度Linux",
		"neokylin":  "中标麒麟",
		"redflag":   "红旗Linux",
		"asianux":   "亚洲服务器",
		"cosmo":     "中科方德",
		"euler":     "欧拉系统",
		"openeuler": "openEuler",
		"anolis":    "龙蜥操作系统",
	}

	for keyword, name := range domesticOSKeywords {
		if strings.Contains(content, keyword) {
			return true, name, nil
		}
	}

	return false, "", nil
}

// 备用检测方法
func (i *Installer) checkAlternativeOSDetection(client *ssh.Client) (bool, string, error) {
	// 检查特有的目录或文件
	domesticPaths := map[string]string{
		"/etc/kylin-release":    "银河麒麟",
		"/etc/uos-release":      "统信UOS",
		"/etc/neokylin-release": "中标麒麟",
		"/etc/redflag-release":  "红旗Linux",
	}

	for path, name := range domesticPaths {
		result, err := client.ExecuteCommand(fmt.Sprintf("test -f %s && echo 'found' || echo 'not_found'", path))
		if err == nil && strings.TrimSpace(result.Stdout) == "found" {
			return true, name, nil
		}
	}

	// 检查uname信息
	result, err := client.ExecuteCommand("uname -a")
	if err == nil {
		unameInfo := strings.ToLower(result.Stdout)
		if strings.Contains(unameInfo, "kylin") ||
			strings.Contains(unameInfo, "uos") ||
			strings.Contains(unameInfo, "neokylin") {
			return true, "国产操作系统", nil
		}
	}

	return false, "", nil
}

// 生成自定义CA证书
func (i *Installer) generateCustomCACerts(client *ssh.Client) error {
	// 这里可以实现自定义CA证书生成逻辑
	// 为了简化，暂时跳过
	i.logger.Info("自定义CA证书生成已跳过（可在需要时实现）")
	return nil
}

// 在setup_env函数的末尾添加setup_registry调用
func (i *Installer) addRegistrySetup(script []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(script))
	var modifiedScript bytes.Buffer

	registrySetupFunction := `
# Setup container registry configuration
setup_registry() {
    if [ -n "${INSTALL_K3S_REGISTRIES}" ]; then
        info "Setting up container registries: ${INSTALL_K3S_REGISTRIES}"
        # Registry configuration logic would go here
    fi
}
`

	// 首先添加registry setup函数
	modifiedScript.WriteString(registrySetupFunction)

	for scanner.Scan() {
		line := scanner.Text()
		modifiedScript.WriteString(line + "\n")

		// 在setup_env函数的末尾调用setup_registry
		if strings.HasPrefix(line, "setup_env() {") {
			for scanner.Scan() {
				line = scanner.Text()
				if line == "}" {
					modifiedScript.WriteString("    setup_registry\n")
					modifiedScript.WriteString(line + "\n")
					break
				}
				modifiedScript.WriteString(line + "\n")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("扫描脚本添加注册表设置时出错: %w", err)
	}

	return modifiedScript.Bytes(), nil
}

// 在create_env_file函数中添加客户端证书有效期环境变量
func (i *Installer) addCertificateConfig(script []byte, clientExpirationYears, daysInYear int) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(script))
	var modifiedScript bytes.Buffer

	// 计算客户端/服务器证书的过期天数
	calculatedCertExpirationDays := clientExpirationYears * daysInYear

	for scanner.Scan() {
		line := scanner.Text()
		modifiedScript.WriteString(line + "\n")

		// 在create_env_file函数的末尾添加客户端证书有效期环境变量
		if strings.HasPrefix(line, "create_env_file() {") {
			for scanner.Scan() {
				line = scanner.Text()
				if line == "}" {
					// 确保这里写入的是计算后的天数
					modifiedScript.WriteString(fmt.Sprintf("    echo 'CATTLE_NEW_SIGNED_CERT_EXPIRATION_DAYS=%d' | $SUDO tee -a ${FILE_K3S_ENV} >/dev/null\n", calculatedCertExpirationDays))
					modifiedScript.WriteString(line + "\n")
					break
				}
				modifiedScript.WriteString(line + "\n")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("扫描脚本添加证书配置时出错: %w", err)
	}

	return modifiedScript.Bytes(), nil
}

// 可选：提供更灵活的接口
func (i *Installer) modifyScriptSelective(script []byte, options ModifyOptions) ([]byte, error) {
	result := script
	var err error

	if options.EnableRegistry {
		result, err = i.addRegistrySetup(result)
		if err != nil {
			return nil, fmt.Errorf("添加注册表设置失败: %w", err)
		}
	}

	if options.EnableCertConfig {
		result, err = i.addCertificateConfig(result, options.ClientExpirationYears, options.DaysInYear)
		if err != nil {
			return nil, fmt.Errorf("添加证书配置失败: %w", err)
		}
	}

	return result, nil
}

func (i *Installer) verifyMasterInstallation(client *ssh.Client) error {
	// 等待K3s完全启动
	i.logger.Info("等待K3s服务启动...")
	time.Sleep(30 * time.Second)

	// 检查K3s服务状态
	result, err := client.ExecuteCommand("systemctl is-active k3s")
	if err != nil || !strings.Contains(result.Stdout, "active") {
		return fmt.Errorf("K3s服务未正常运行: %s", result.Stderr)
	}

	// 检查kubectl可用性
	result, err = client.ExecuteCommand("kubectl get nodes")
	if err != nil {
		return fmt.Errorf("kubectl命令执行失败: %v", err)
	}

	// 检查Master节点是否Ready
	if !strings.Contains(result.Stdout, "Ready") {
		return fmt.Errorf("Master节点状态异常: %s", result.Stdout)
	}

	return nil
}
