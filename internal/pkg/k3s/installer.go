package k3s

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"path"
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
	caExpirationYears        = 1000 // CA 证书有效期 10 年
	clientExpirationYears    = 100  // 客户端证书有效期 10 年
	daysInYear               = 365  // 每年近似天数，用于证书有效期计算
	keyBits                  = 2048
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

// CertificateAuthority 表示一个 CA
type CertificateAuthority struct {
	Cert       *x509.Certificate
	PrivateKey *rsa.PrivateKey
}

// CertConfig 证书配置
type CertConfig struct {
	KeyFile  string
	CertFile string
	CN       string
	Dir      string
	IsCA     bool
	Usage    []x509.ExtKeyUsage
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

	// 设置环境变量，仅包含节点名称
	envArgs := []string{
		"K3S_NODE_NAME=k3s-master",
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

func (i *Installer) InstallAgent(client *ssh.Client, masterClient *ssh.Client, nodeName string, token string) error {
	i.logger.Infof("开始在节点 %s 上安装K3s Agent", nodeName)

	// 检查是否已经安装K3s
	if result, err := client.ExecuteCommand("which k3s"); err == nil && result.Stdout != "" {
		i.logger.Warnf("节点 %s 已经安装了K3s，跳过安装步骤", nodeName)
		return nil
	}

	// 获取Master内部IP
	masterIP, err := i.getInternalIP(masterClient)
	if err != nil {
		return fmt.Errorf("获取Master内部IP失败: %v", err)
	}
	i.logger.Infof("从Master节点自动获取的内部IP: %s", masterIP)

	// 设置环境变量，包含节点名称
	envArgs := []string{
		fmt.Sprintf("K3S_URL=https://%s:6443", masterIP),
		fmt.Sprintf("K3S_TOKEN=%s", token),
		fmt.Sprintf("K3S_NODE_NAME=%s", nodeName),
	}
	cmdArgs := []string{}

	if err := i.autoInstallK3sByLocation(client, envArgs, cmdArgs); err != nil {
		return fmt.Errorf("K3s Agent安装失败: %v", err)
	}

	// 验证 Agent 安装
	if err := i.verifyAgentInstallation(client); err != nil {
		return fmt.Errorf("验证Agent安装失败: %v", err)
	}

	i.logger.Infof("节点 %s K3s Agent安装成功", nodeName)
	return nil
}

func (i *Installer) getInternalIP(client *ssh.Client) (string, error) {
	cmd := `bash -c "echo '' | nc -u -w 2 8.8.8.8 80 && ip -4 addr show | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | grep -v '^127\.' | head -n 1"`
	result, err := client.ExecuteCommand(cmd)
	if err != nil {
		// 备选命令，防止 nc 不可用
		cmd = `ip route get 8.8.8.8 | grep -oP 'src \K\d+(\.\d+){3}' | head -n 1`
		result, err = client.ExecuteCommand(cmd)
		if err != nil {
			return "", fmt.Errorf("执行IP获取命令失败: %v", err)
		}
	}

	ip := strings.TrimSpace(result.Stdout)
	if ip == "" {
		return "", fmt.Errorf("无法获取节点的内部IP")
	}

	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("获取的IP地址格式无效: %s", ip)
	}

	return ip, nil
}

func (i *Installer) autoInstallK3sByLocation(client *ssh.Client, envArgs, cmdArgs []string) error {
	installURL, err := i.getInstallURL(client)
	if err != nil {
		return err
	}

	i.logger.Infof("使用安装URL: %s", installURL)
	return i.executeInstall(client, installURL, envArgs, cmdArgs)
}

func (i *Installer) getInstallURL(client *ssh.Client) (string, error) {
	if isChina, err := i.isInMainlandChina(client); err != nil {
		i.logger.Warnf("无法判断网络环境，默认使用国内源: %v", err)
		return officialCNInstallURL, nil
	} else if isChina {
		return officialCNInstallURL, nil
	}
	return officialInstallURL, nil
}

func (i *Installer) isInMainlandChina(client *ssh.Client) (bool, error) {
	if reachable, _ := i.isInternetReachable(client, "http://www.baidu.com"); !reachable {
		return true, nil
	}
	if reachable, _ := i.isInternetReachable(client, "http://www.google.com"); !reachable {
		return true, nil
	}
	return false, nil
}

func (i *Installer) isInternetReachable(client *ssh.Client, url string) (bool, error) {
	cmd := fmt.Sprintf("curl -s --connect-timeout 3 --max-time 5 %s > /dev/null 2>&1", url)
	result, err := client.ExecuteCommand(cmd)
	return err == nil && result.ExitCode == 0, err
}

func (i *Installer) executeInstall(client *ssh.Client, installURL string, envArgs, cmdArgs []string) error {
	i.logger.Infof("=== K3s 安装调试信息 ===")
	i.logger.Infof("安装URL: %s", installURL)
	i.logger.Warnf("脚本在后端下载，确保 %s 适合目标节点网络环境", installURL)
	i.logger.Infof("环境变量数量: %d", len(envArgs))
	i.logger.Infof("命令参数数量: %d", len(cmdArgs))

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

	// 脚本预览
	scriptLines := strings.Split(string(modifiedScript), "\n")
	i.logger.Info("脚本预览（前3行）：")
	for idx := 0; idx < 3 && idx < len(scriptLines); idx++ {
		i.logger.Infof("  %d: %s", idx+1, scriptLines[idx])
	}
	if len(scriptLines) > 6 {
		i.logger.Infof("  ... (%d 行省略) ...", len(scriptLines)-6)
	}
	i.logger.Info("脚本预览（后3行）：")
	start := len(scriptLines) - 3
	if start < 3 {
		start = 3
	}
	for idx := start; idx < len(scriptLines); idx++ {
		if idx >= 0 && scriptLines[idx] != "" {
			i.logger.Infof("  %d: %s", idx+1, scriptLines[idx])
		}
	}

	isAgentMode := false
	for _, env := range envArgs {
		if strings.Contains(env, "K3S_URL=") {
			isAgentMode = true
			break
		}
	}
	if !isAgentMode {
		i.logger.Info("Step 3: 生成自定义CA证书")
		if err := i.generateCustomCACerts(client); err != nil {
			i.logger.Warnf("生成自定义CA证书失败: %v", err)
		}
	} else {
		i.logger.Info("Step 3: 跳过自定义CA证书生成（Agent 模式）")
	}

	i.logger.Info("Step 4: 准备环境变量和参数")
	finalEnvArgs := make([]string, len(envArgs))
	copy(finalEnvArgs, envArgs)
	finalCmdArgs := make([]string, len(cmdArgs))
	copy(finalCmdArgs, cmdArgs)

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

	if installURL == officialCNInstallURL {
		i.logger.Info("--- 国内镜像配置 ---")

		additionalEnvs := []string{
			"INSTALL_K3S_MIRROR=cn",
			fmt.Sprintf("INSTALL_K3S_REGISTRIES=%s", additionalRegistryURLs),
		}
		finalEnvArgs = append(finalEnvArgs, additionalEnvs...)

		isAgentMode := false
		for _, env := range finalEnvArgs {
			if strings.Contains(env, "K3S_URL=") {
				isAgentMode = true
				break
			}
		}

		additionalArgs := []string{}
		if !isAgentMode {
			additionalArgs = []string{
				fmt.Sprintf("--system-default-registry=%s", defaultSystemRegistryURL),
				"--disable-default-registry-endpoint",
			}
			i.logger.Info("已添加国内镜像命令参数（仅 Server 模式）")
		} else {
			i.logger.Info("跳过国内镜像命令参数（Agent 模式）")
		}
		finalCmdArgs = append(finalCmdArgs, additionalArgs...)
	}

	i.logger.Infof("最终环境变量: %d 总计", len(finalEnvArgs))
	for idx, env := range finalEnvArgs {
		if strings.Contains(strings.ToUpper(env), "TOKEN") || strings.Contains(strings.ToUpper(env), "PASSWORD") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				i.logger.Infof("  [%d] %s=***HIDDEN***", idx, parts[0])
			} else {
				i.logger.Infof("  [%d] %s", idx, env)
			}
		} else {
			i.logger.Infof("  [%d] %s", idx, env)
		}
	}

	i.logger.Infof("最终命令参数: %d 总计", len(finalCmdArgs))
	for idx, arg := range finalCmdArgs {
		i.logger.Infof("  [%d] %s", idx, arg)
	}

	i.logger.Info("Step 5: 构建Shell命令")
	shellArgs := []string{"-s"}
	if len(finalCmdArgs) > 0 {
		shellArgs = append(shellArgs, "--")
		shellArgs = append(shellArgs, finalCmdArgs...)
	}

	cmd := "/bin/sh " + strings.Join(shellArgs, " ")
	i.logger.Infof("Shell命令: %s", cmd)
	i.logger.Info("Shell参数分解：")
	for idx, arg := range shellArgs {
		switch arg {
		case "-s":
			i.logger.Infof("  [%d] %s  (从stdin读取脚本)", idx, arg)
		case "--":
			i.logger.Infof("  [%d] %s  (分隔符：后续参数传递给脚本)", idx, arg)
		default:
			i.logger.Infof("  [%d] %s  (作为$%d传递给脚本)", idx, arg, idx-1)
		}
	}

	i.logger.Info("Step 6: 开始执行安装")
	i.logger.Infof("等效官方安装命令：")
	i.logger.Infof("  curl -sfL %s | %s sh -s - %s", installURL, strings.Join(finalEnvArgs, " "), strings.Join(finalCmdArgs, " "))
	result, err := client.ExecuteCommandWithStdin(modifiedScript, cmd, finalEnvArgs)
	if err != nil {
		i.logger.Errorf("K3s安装失败: %v", err)
		if result != nil {
			i.logger.Errorf("标准输出: %s", result.Stdout)
			i.logger.Errorf("错误输出: %s", result.Stderr)
		} else {
			i.logger.Errorf("无标准输出或错误输出（result is nil）")
		}
		if isDomestic {
			i.logger.Info("💡 注意：已为国产操作系统启用SELinux绕过 (%s)", osName)
			i.logger.Info("💡 如果问题持续，问题可能与SELinux无关")
		}
		return fmt.Errorf("K3s安装失败: %v", err)
	}

	i.logger.Infof("安装脚本输出: %s", result.Stdout)
	i.logger.Info("K3s安装完成!")
	if isDomestic {
		i.logger.Infof("国产操作系统 (%s) 兼容模式已使用", osName)
	}
	return nil
}

func (i *Installer) isDomesticOS(client *ssh.Client) (bool, string, error) {
	result, err := client.ExecuteCommand("cat /etc/os-release 2>/dev/null || echo 'not_found'")
	if err != nil {
		return i.checkAlternativeOSDetection(client)
	}

	if result.Stdout == "not_found" {
		return i.checkAlternativeOSDetection(client)
	}

	content := strings.ToLower(result.Stdout)

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

func (i *Installer) checkAlternativeOSDetection(client *ssh.Client) (bool, string, error) {
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

func (i *Installer) addRegistrySetup(script []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(script))
	var modifiedScript bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()
		modifiedScript.WriteString(line + "\n")

		if strings.HasPrefix(line, "setup_env() {") {
			for scanner.Scan() {
				line := scanner.Text()
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
		return nil, fmt.Errorf("error scanning script for registry setup: %w", err)
	}

	return modifiedScript.Bytes(), nil
}

func (i *Installer) addCertificateConfig(script []byte, clientExpirationYears, daysInYear int) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(script))
	var modifiedScript bytes.Buffer

	calculatedCertExpirationDays := clientExpirationYears * daysInYear

	for scanner.Scan() {
		line := scanner.Text()
		modifiedScript.WriteString(line + "\n")

		if strings.HasPrefix(line, "create_env_file() {") {
			for scanner.Scan() {
				line := scanner.Text()
				if line == "}" {
					modifiedScript.WriteString(fmt.Sprintf("    echo 'CATTLE_NEW_SIGNED_CERT_EXPIRATION_DAYS=%d' | $SUDO tee -a ${FILE_K3S_ENV} >/dev/null\n", calculatedCertExpirationDays))
					modifiedScript.WriteString(line + "\n")
					break
				}
				modifiedScript.WriteString(line + "\n")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning script for certificate config: %w", err)
	}

	return modifiedScript.Bytes(), nil
}

func (i *Installer) modifyScriptSelective(script []byte, options ModifyOptions) ([]byte, error) {
	result := script
	var err error

	if options.EnableRegistry {
		result, err = i.addRegistrySetup(result)
		if err != nil {
			return nil, fmt.Errorf("failed to add registry setup: %w", err)
		}
	}

	if options.EnableCertConfig {
		result, err = i.addCertificateConfig(result, options.ClientExpirationYears, options.DaysInYear)
		if err != nil {
			return nil, fmt.Errorf("failed to add certificate config: %w", err)
		}
	}

	return result, nil
}

func (i *Installer) verifyMasterInstallation(client *ssh.Client) error {
	i.logger.Info("等待K3s服务启动...")
	// 增加重试机制，最多等待3分钟
	for attempt := 0; attempt < 18; attempt++ {
		result, err := client.ExecuteCommand("systemctl is-active k3s")
		if err == nil && strings.Contains(result.Stdout, "active") {
			i.logger.Info("K3s服务已启动")
			break
		}
		i.logger.Warnf("K3s服务未就绪（尝试 %d/%d）: %v, Stdout: %s, Stderr: %s", attempt+1, 18, err, result.Stdout, result.Stderr)
		time.Sleep(10 * time.Second)
	}

	result, err := client.ExecuteCommand("systemctl is-active k3s")
	if err != nil || !strings.Contains(result.Stdout, "active") {
		// 获取更多服务状态信息
		logResult, logErr := client.ExecuteCommand("journalctl -u k3s.service -n 50")
		if logErr == nil {
			i.logger.Errorf("K3s服务日志: %s", logResult.Stdout)
		}
		return fmt.Errorf("K3s服务未正常运行: %v, Stderr: %s", err, result.Stderr)
	}

	result, err = client.ExecuteCommand("kubectl get nodes")
	if err != nil {
		return fmt.Errorf("kubectl命令执行失败: %v", err)
	}

	if !strings.Contains(result.Stdout, "Ready") {
		return fmt.Errorf("Master节点状态异常: %s", result.Stdout)
	}

	return nil
}

func (i *Installer) verifyAgentInstallation(client *ssh.Client) error {
	i.logger.Info("等待K3s Agent服务启动...")
	// 增加重试机制，最多等待3分钟
	for attempt := 0; attempt < 18; attempt++ {
		result, err := client.ExecuteCommand("systemctl is-active k3s-agent")
		if err == nil && strings.Contains(result.Stdout, "active") {
			i.logger.Info("K3s Agent服务已启动")
			break
		}
		i.logger.Warnf("K3s Agent服务未就绪（尝试 %d/%d）: %v, Stdout: %s, Stderr: %s", attempt+1, 18, err, result.Stdout, result.Stderr)
		time.Sleep(10 * time.Second)
	}

	result, err := client.ExecuteCommand("systemctl is-active k3s-agent")
	if err != nil || !strings.Contains(result.Stdout, "active") {
		// 获取更多服务状态信息
		logResult, logErr := client.ExecuteCommand("journalctl -u k3s-agent.service -n 50")
		if logErr == nil {
			i.logger.Errorf("K3s Agent服务日志: %s", logResult.Stdout)
		}
		return fmt.Errorf("K3s Agent服务未正常运行: %v, Stderr: %s", err, result.Stderr)
	}

	return nil
}

// generatePrivateKey 生成 RSA 私钥
func generatePrivateKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, keyBits)
}

// createCertificateTemplate 创建证书模板
func createCertificateTemplate(cn string, isCA bool, usage []x509.ExtKeyUsage) (*x509.Certificate, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %v", err)
	}

	now := time.Now()
	var notAfter time.Time
	if isCA {
		// CA 证书有效期 10 年
		notAfter = now.AddDate(caExpirationYears, 0, 0)
	} else {
		// 客户端证书有效期 10 年
		notAfter = now.AddDate(clientExpirationYears, 0, 0)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:             now,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           usage,
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}

	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}

	return template, nil
}

// generateCA 生成 CA 证书
func generateCA(cn string) (*CertificateAuthority, error) {
	// 生成私钥
	privateKey, err := generatePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %v", err)
	}

	// 创建证书模板
	template, err := createCertificateTemplate(cn, true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate template: %v", err)
	}

	// 生成自签名证书
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %v", err)
	}

	// 解析证书
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %v", err)
	}

	return &CertificateAuthority{
		Cert:       cert,
		PrivateKey: privateKey,
	}, nil
}

// generateClientCert 生成客户端证书
func generateClientCert(cn string, ca *CertificateAuthority, usage []x509.ExtKeyUsage) (*x509.Certificate, *rsa.PrivateKey, error) {
	// 生成私钥
	privateKey, err := generatePrivateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %v", err)
	}

	// 创建证书模板
	template, err := createCertificateTemplate(cn, false, usage)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate template: %v", err)
	}

	// 使用 CA 签名
	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.Cert, &privateKey.PublicKey, ca.PrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %v", err)
	}

	// 解析证书
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse certificate: %v", err)
	}

	return cert, privateKey, nil
}

// saveCertificateAndKey 保存证书和私钥到远程节点
func saveCertificateAndKey(cert *x509.Certificate, privateKey *rsa.PrivateKey, certPath, keyPath string, client *ssh.Client) error {
	// 编码证书
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	// 编码私钥
	privKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privKeyBytes})

	// 上传证书文件
	if err := client.UploadFile(string(certPEM), certPath); err != nil {
		return fmt.Errorf("failed to upload certificate file %s: %v", certPath, err)
	}

	// 上传私钥文件
	if err := client.UploadFile(string(keyPEM), keyPath); err != nil {
		return fmt.Errorf("failed to upload private key file %s: %v", keyPath, err)
	}

	// 设置文件权限
	if _, err := client.ExecuteCommand(fmt.Sprintf("chmod 644 %s", certPath)); err != nil {
		return fmt.Errorf("failed to set permissions for certificate file %s: %v", certPath, err)
	}
	if _, err := client.ExecuteCommand(fmt.Sprintf("chmod 600 %s", keyPath)); err != nil {
		return fmt.Errorf("failed to set permissions for private key file %s: %v", keyPath, err)
	}

	return nil
}

// generateCustomCACerts 生成自定义 CA 证书
func (i *Installer) generateCustomCACerts(client *ssh.Client) error {
	i.logger.Info("开始生成自定义 CA 证书")

	// 主证书目录
	//certDir := "/var/lib/rancher/k3s/server/tls"
	//etcdCertDir := "/var/lib/rancher/k3s/server/tls/etcd"
	// 主证书目录（使用 / 开头，确保绝对路径）
	certDir := "/var/lib/rancher/k3s/server/tls"
	etcdCertDir := path.Join(certDir, "etcd") // 使用 path.Join，确保 /

	// 确保证书目录存在
	if _, err := client.ExecuteCommand(fmt.Sprintf("mkdir -p %s", certDir)); err != nil {
		return fmt.Errorf("failed to create certificate directory %s: %v", certDir, err)
	}
	if _, err := client.ExecuteCommand(fmt.Sprintf("mkdir -p %s", etcdCertDir)); err != nil {
		return fmt.Errorf("failed to create ETCD certificate directory %s: %v", etcdCertDir, err)
	}

	// 设置目录权限
	if _, err := client.ExecuteCommand(fmt.Sprintf("chmod 755 %s", certDir)); err != nil {
		return fmt.Errorf("failed to set permissions for certificate directory %s: %v", certDir, err)
	}
	if _, err := client.ExecuteCommand(fmt.Sprintf("chmod 755 %s", etcdCertDir)); err != nil {
		return fmt.Errorf("failed to set permissions for ETCD certificate directory %s: %v", etcdCertDir, err)
	}

	// 定义需要生成的 CA 证书
	caConfigs := []CertConfig{
		{KeyFile: "client-ca.key", CertFile: "client-ca.crt", CN: "k3s-client-ca", Dir: certDir, IsCA: true, Usage: nil},
		{KeyFile: "server-ca.key", CertFile: "server-ca.crt", CN: "k3s-server-ca", Dir: certDir, IsCA: true, Usage: nil},
		{KeyFile: "request-header-ca.key", CertFile: "request-header-ca.crt", CN: "k3s-request-header-ca", Dir: certDir, IsCA: true, Usage: nil},
		{KeyFile: "server-ca.key", CertFile: "server-ca.crt", CN: "etcd-server-ca", Dir: etcdCertDir, IsCA: true, Usage: nil},
		{KeyFile: "peer-ca.key", CertFile: "peer-ca.crt", CN: "etcd-peer-ca", Dir: etcdCertDir, IsCA: true, Usage: nil},
	}

	// 存储生成的 CA
	cas := make(map[string]*CertificateAuthority)

	// 生成 CA 证书
	for _, config := range caConfigs {
		i.logger.Infof("Generating CA certificate: %s", config.CN)

		ca, err := generateCA(config.CN)
		if err != nil {
			return fmt.Errorf("failed to generate CA %s: %v", config.CN, err)
		}

		//keyPath := filepath.Join(config.Dir, config.KeyFile)
		//certPath := filepath.Join(config.Dir, config.CertFile)
		keyPath := path.Join(config.Dir, config.KeyFile)   // 修改：使用 path.Join
		certPath := path.Join(config.Dir, config.CertFile) // 修改：使用 path.Join
		keyPath = path.Clean(keyPath)                      // 确保清洁路径
		certPath = path.Clean(certPath)

		if err := saveCertificateAndKey(ca.Cert, ca.PrivateKey, certPath, keyPath, client); err != nil {
			return fmt.Errorf("failed to save CA %s: %v", config.CN, err)
		}

		// 存储 CA 用于后续签名
		cas[config.CN] = ca
		i.logger.Infof("Generated CA certificate: %s", certPath)
	}

	// 生成 ETCD 客户端证书
	etcdServerCA := cas["etcd-server-ca"]
	etcdPeerCA := cas["etcd-peer-ca"]

	// ETCD 客户端证书配置
	clientCerts := []struct {
		CN       string
		KeyFile  string
		CertFile string
		CA       *CertificateAuthority
		Usage    []x509.ExtKeyUsage
	}{
		{
			CN:       "etcd-client",
			KeyFile:  "client.key",
			CertFile: "client.crt",
			CA:       etcdServerCA,
			Usage:    []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		},
		{
			CN:       "etcd-server",
			KeyFile:  "server-client.key",
			CertFile: "server-client.crt",
			CA:       etcdServerCA,
			Usage:    []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		},
		{
			CN:       "etcd-peer",
			KeyFile:  "peer-server-client.key",
			CertFile: "peer-server-client.crt",
			CA:       etcdPeerCA,
			Usage:    []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		},
	}

	// 生成客户端证书
	for _, config := range clientCerts {
		i.logger.Infof("Generating client certificate: %s", config.CN)

		cert, privateKey, err := generateClientCert(config.CN, config.CA, config.Usage)
		if err != nil {
			return fmt.Errorf("failed to generate client certificate %s: %v", config.CN, err)
		}

		//keyPath := filepath.Join(etcdCertDir, config.KeyFile)
		//certPath := filepath.Join(etcdCertDir, config.CertFile)
		keyPath := path.Join(etcdCertDir, config.KeyFile)   // 修改：使用 path.Join
		certPath := path.Join(etcdCertDir, config.CertFile) // 修改：使用 path.Join
		keyPath = path.Clean(keyPath)
		certPath = path.Clean(certPath)

		if err := saveCertificateAndKey(cert, privateKey, certPath, keyPath, client); err != nil {
			return fmt.Errorf("failed to save client certificate %s: %v", config.CN, err)
		}

		i.logger.Infof("Generated client certificate: %s", certPath)
	}

	i.logger.Info("自定义 CA 证书和 ETCD 证书生成成功")
	return nil
}
