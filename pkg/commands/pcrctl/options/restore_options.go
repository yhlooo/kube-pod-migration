package options

import "github.com/spf13/pflag"

// NewDefaultRestoreOptions 返回一个默认的 RestoreOptions
func NewDefaultRestoreOptions() RestoreOptions {
	return RestoreOptions{
		ContainerRuntime:         "containerd",
		ContainerRuntimeEndpoint: "unix:///run/containerd/containerd.sock",
	}
}

// RestoreOptions restore 子命令选项
type RestoreOptions struct {
	// 容器运行时
	ContainerRuntime string `json:"containerRuntime,omitempty" yaml:"containerRuntime,omitempty"`
	// 容器运行时访问入口
	ContainerRuntimeEndpoint string `json:"containerRuntimeEndpoint,omitempty" yaml:"containerRuntimeEndpoint,omitempty"`
}

// AddPFlags 将选项绑定到命令行参数
func (o *RestoreOptions) AddPFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.ContainerRuntime, "runtime", o.ContainerRuntime, "Container runtime")
	flags.StringVar(&o.ContainerRuntimeEndpoint, "endpoint", o.ContainerRuntimeEndpoint, "Container runtime endpoint")
}
