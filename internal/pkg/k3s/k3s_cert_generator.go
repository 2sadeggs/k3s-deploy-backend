package k3s

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"path"
	"time"

	"k3s-deploy-backend/internal/pkg/ssh"
)

const (
	caExpirationYears     = 1000 // CA 证书有效期 10 年
	clientExpirationYears = 100  // 客户端证书有效期 10 年
	daysInYear            = 365  // 每年近似天数，用于证书有效期计算
	keyBits               = 2048
)

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
