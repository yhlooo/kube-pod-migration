package containerd

import runtimev1 "k8s.io/cri-api/pkg/apis/runtime/v1"

const (
	mediaTypeContainerSpec = "application/vnd.containerd.container.checkpoint.config.v1+proto"
)

// SandboxInfo runtimev1.PodSandboxStatusResponse.Info["info"] 的部分结构
type SandboxInfo struct {
	Pid    int                         `json:"pid"`
	Config *runtimev1.PodSandboxConfig `json:"config,omitempty"`
}
