package containerd

import (
	ociruntime "github.com/opencontainers/runtime-spec/specs-go"
	runtimev1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	mediaTypeContainerSpec = "application/vnd.containerd.container.checkpoint.config.v1+proto"
)

// SandboxInfo 沙盒信息
type SandboxInfo struct {
	// 沙盒 ID
	ID string `json:"id"`

	// 以下字段是 runtimev1.PodSandboxStatusResponse.Info["info"] 的部分结构

	// 沙盒容器 1 号进程在宿主机上的 Pid
	Pid int `json:"pid"`
	// 沙盒配置
	Config *runtimev1.PodSandboxConfig `json:"config,omitempty"`
	// 沙盒运行时配置
	RuntimeSpec *ociruntime.Spec `json:"runtimeSpec,omitempty"`
}
