package service

import (
	"fmt"

	"k3s-deploy-backend/internal/model"
	"k3s-deploy-backend/internal/pkg/logger"
)

type DeployService struct {
	sshService *SSHService
	k3sService *K3sService
	logger     *logger.Logger
}

func NewDeployService(sshService *SSHService, k3sService *K3sService, logger *logger.Logger) *DeployService {
	return &DeployService{
		sshService: sshService,
		k3sService: k3sService,
		logger:     logger,
	}
}

var stepHandlers = map[string]func(*DeployService, *model.DeployRequest) error{
	"validate":        (*DeployService).validateStep,
	"install-master":  (*DeployService).installMasterStep,
	"configure-agent": (*DeployService).configureAgentStep,
	"apply-labels":    (*DeployService).applyLabelsStep,
	"deploy-insuite":  (*DeployService).deployInSuiteStep,
	"verify":          (*DeployService).verifyStep,
}

func (s *DeployService) ExecuteStep(req *model.DeployRequest) *model.DeployResponse {
	s.logger.Infof("执行部署步骤: %s", req.Step)

	handler, exists := stepHandlers[req.Step]
	if !exists {
		s.logger.Errorf("未知的部署步骤: %s", req.Step)
		return &model.DeployResponse{
			Success: false,
			Message: fmt.Sprintf("未知的部署步骤: %s", req.Step),
		}
	}

	if err := handler(s, req); err != nil {
		s.logger.DeploymentError(req.Step, err)
		return &model.DeployResponse{
			Success: false,
			Message: err.Error(),
			Step:    req.Step,
		}
	}

	s.logger.DeploymentSuccess(req.Step)
	return &model.DeployResponse{
		Success: true,
		Message: fmt.Sprintf("步骤 %s 执行成功", req.Step),
		Step:    req.Step,
	}
}

func (s *DeployService) validateStep(req *model.DeployRequest) error {
	return s.k3sService.ValidateNodes(req.Nodes)
}

func (s *DeployService) installMasterStep(req *model.DeployRequest) error {
	// 找到Master节点
	var masterNode model.NodeConfig
	for _, node := range req.Nodes {
		if node.Name == "k3s-master" {
			masterNode = node
			break
		}
	}

	if masterNode.Name == "" {
		return fmt.Errorf("未找到Master节点")
	}

	return s.k3sService.InstallMaster(masterNode)
}

func (s *DeployService) configureAgentStep(req *model.DeployRequest) error {
	// 找到Master节点
	var masterNode model.NodeConfig
	for _, node := range req.Nodes {
		if node.Name == "k3s-master" {
			masterNode = node
			break
		}
	}

	if masterNode.Name == "" {
		return fmt.Errorf("未找到Master节点")
	}

	// 配置所有Agent节点，使用索引生成节点名称
	agentIndex := 0
	for _, node := range req.Nodes {
		if node.Name != "k3s-master" {
			if err := s.k3sService.ConfigureAgent(masterNode, node, agentIndex); err != nil {
				return fmt.Errorf("配置Agent节点 %s 失败: %v", node.Name, err)
			}
			agentIndex++
		}
	}

	return nil
}

func (s *DeployService) applyLabelsStep(req *model.DeployRequest) error {
	// 找到Master节点
	var masterNode model.NodeConfig
	for _, node := range req.Nodes {
		if node.Name == "k3s-master" {
			masterNode = node
			break
		}
	}

	if masterNode.Name == "" {
		return fmt.Errorf("未找到Master节点")
	}

	return s.k3sService.ApplyLabels(masterNode, req.Labels)
}

func (s *DeployService) deployInSuiteStep(req *model.DeployRequest) error {
	// 找到Master节点
	var masterNode model.NodeConfig
	for _, node := range req.Nodes {
		if node.Name == "k3s-master" {
			masterNode = node
			break
		}
	}

	if masterNode.Name == "" {
		return fmt.Errorf("未找到Master节点")
	}

	return s.k3sService.DeployInSuite(masterNode, req.RoleAssignment)
}

func (s *DeployService) verifyStep(req *model.DeployRequest) error {
	// 找到Master节点
	var masterNode model.NodeConfig
	for _, node := range req.Nodes {
		if node.Name == "k3s-master" {
			masterNode = node
			break
		}
	}

	if masterNode.Name == "" {
		return fmt.Errorf("未找到Master节点")
	}

	return s.k3sService.VerifyDeployment(masterNode)
}
