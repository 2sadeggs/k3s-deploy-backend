package utils

import "fmt"

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

func (e *APIError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Details)
	}
	return e.Message
}

func NewSSHError(err error) *APIError {
	return &APIError{
		Code:    1001,
		Message: "SSH连接错误",
		Details: err.Error(),
	}
}

func NewDeployError(step string, err error) *APIError {
	return &APIError{
		Code:    2001,
		Message: fmt.Sprintf("部署步骤 %s 失败", step),
		Details: err.Error(),
	}
}

func NewValidationError(field string, value interface{}) *APIError {
	return &APIError{
		Code:    3001,
		Message: fmt.Sprintf("参数验证失败: %s", field),
		Details: fmt.Sprintf("无效的值: %v", value),
	}
}

func NewK3sError(operation string, err error) *APIError {
	return &APIError{
		Code:    4001,
		Message: fmt.Sprintf("K3s操作失败: %s", operation),
		Details: err.Error(),
	}
}

func NewSystemError(err error) *APIError {
	return &APIError{
		Code:    5001,
		Message: "系统错误",
		Details: err.Error(),
	}
}
