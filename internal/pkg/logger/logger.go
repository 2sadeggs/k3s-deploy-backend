package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

type Logger struct {
	*logrus.Logger
}

func NewLogger() *Logger {
	logger := logrus.New()

	// 设置日志格式
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		ForceColors:   true,
	})

	// 设置日志级别
	logger.SetLevel(logrus.InfoLevel)

	// 设置输出
	logger.SetOutput(os.Stdout)

	return &Logger{Logger: logger}
}

func (l *Logger) SSHConnectionAttempt(connType, target string) {
	l.WithFields(logrus.Fields{
		"type":   "ssh_connection",
		"method": connType,
		"target": target,
	}).Info("尝试SSH连接")
}

func (l *Logger) DeploymentStep(step, node string) {
	l.WithFields(logrus.Fields{
		"type": "deployment",
		"step": step,
		"node": node,
	}).Info("执行部署步骤")
}

func (l *Logger) DeploymentError(step string, err error) {
	l.WithFields(logrus.Fields{
		"type":  "deployment",
		"step":  step,
		"error": err.Error(),
	}).Error("部署步骤失败")
}

func (l *Logger) DeploymentSuccess(step string) {
	l.WithFields(logrus.Fields{
		"type": "deployment",
		"step": step,
	}).Info("部署步骤成功")
}
