package options

import (
	"github.com/spf13/pflag"
)

// NewDefaultCheckpointOptions 返回一个默认的 CheckpointOptions
func NewDefaultCheckpointOptions() CheckpointOptions {
	return CheckpointOptions{
		Namespace:                "default",
		ContainerRuntime:         "containerd",
		ContainerRuntimeEndpoint: "unix:///run/containerd/containerd.sock",
		ExportFile:               "",
		RetainCheckpointImages:   false,
	}
}

// CheckpointOptions checkpoint 子命令选项
type CheckpointOptions struct {
	// Pod 命名空间
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	// 容器运行时
	ContainerRuntime string `json:"containerRuntime,omitempty" yaml:"containerRuntime,omitempty"`
	// 容器运行时访问入口
	ContainerRuntimeEndpoint string `json:"containerRuntimeEndpoint,omitempty" yaml:"containerRuntimeEndpoint,omitempty"`
	// 检查点导出目录
	ExportFile string `json:"exportFile,omitempty" yaml:"exportFile,omitempty"`
	// 导出后容器检查点后保留检查点镜像
	RetainCheckpointImages bool `json:"retainCheckpointImages,omitempty" yaml:"retainCheckpointImages,omitempty"`
}

// AddPFlags 将选项绑定到命令行参数
func (o *CheckpointOptions) AddPFlags(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Namespace, "namespace", "n", o.Namespace, "Pod namespace")
	flags.StringVar(&o.ContainerRuntime, "runtime", o.ContainerRuntime, "Container runtime")
	flags.StringVar(&o.ContainerRuntimeEndpoint, "endpoint", o.ContainerRuntimeEndpoint, "Container runtime endpoint")
	flags.StringVar(&o.ExportFile, "export", o.ExportFile, "Tar file to export checkpoint")
	flags.BoolVar(
		&o.RetainCheckpointImages, "retain-checkpoint-images", o.RetainCheckpointImages,
		"Retain checkpoint images after export",
	)
}
