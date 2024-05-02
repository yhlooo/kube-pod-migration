package options

import "github.com/spf13/pflag"

// NewDefaultCheckpointOptions 返回一个默认的 CheckpointOptions
func NewDefaultCheckpointOptions() CheckpointOptions {
	return CheckpointOptions{}
}

// CheckpointOptions checkpoint 子命令选项
type CheckpointOptions struct{}

// AddPFlags 将选项绑定到命令行参数
func (o *CheckpointOptions) AddPFlags(flags *pflag.FlagSet) {
}
