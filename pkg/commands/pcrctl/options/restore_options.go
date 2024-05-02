package options

import "github.com/spf13/pflag"

// NewDefaultRestoreOptions 返回一个默认的 RestoreOptions
func NewDefaultRestoreOptions() RestoreOptions {
	return RestoreOptions{}
}

// RestoreOptions restore 子命令选项
type RestoreOptions struct{}

// AddPFlags 将选项绑定到命令行参数
func (o *RestoreOptions) AddPFlags(flags *pflag.FlagSet) {
}
