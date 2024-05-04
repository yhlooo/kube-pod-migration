package containerd

import (
	"fmt"
	"strings"
	"time"

	"github.com/containerd/containerd"
	"go.opentelemetry.io/otel/trace/noop"
	criapis "k8s.io/cri-api/pkg/apis"
	"k8s.io/kubernetes/pkg/kubelet/cri/remote"

	"github.com/yhlooo/podmig/pkg/podcr/common"
)

const (
	defaultCRIConnectionTimeout = 2 * time.Second
	defaultContainerdNamespace  = "k8s.io"
	sandboxConfigJSONName       = "sandbox_config.json"
)

// Manager 基于 containerd 的 common.PodCRManager 的实现
type Manager struct {
	tmpdir           string
	criClient        criapis.RuntimeService
	containerdClient *containerd.Client
}

var _ common.PodCRManager = &Manager{}

// New 创建一个 *Manager
func New(endpoint, tmpdir string) (*Manager, error) {
	criClient, err := getCRIClient(endpoint)
	if err != nil {
		return nil, fmt.Errorf("create cri client error: %w", err)
	}
	containerdClient, err := getContainerdClient(endpoint)
	if err != nil {
		return nil, fmt.Errorf("create containerd client error: %w", err)
	}

	return &Manager{
		criClient:        criClient,
		containerdClient: containerdClient,
		tmpdir:           tmpdir,
	}, nil
}

// getCRIClient 获取 CRI 客户端
func getCRIClient(endpoint string) (criapis.RuntimeService, error) {
	tp := noop.NewTracerProvider()
	return remote.NewRemoteRuntimeService(endpoint, defaultCRIConnectionTimeout, tp)
}

// getContainerdClient 获取 containerd 客户端
func getContainerdClient(endpoint string) (*containerd.Client, error) {
	opts := []containerd.ClientOpt{
		containerd.WithDefaultNamespace(defaultContainerdNamespace),
	}
	endpoint = strings.TrimPrefix(endpoint, "unix://")
	return containerd.New(endpoint, opts...)
}
