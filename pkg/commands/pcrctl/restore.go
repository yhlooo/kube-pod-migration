package pcrctl

import (
	"github.com/spf13/cobra"

	"github.com/yhlooo/podmig/pkg/commands/pcrctl/options"
)

// NewRestoreCommandWithOptions 基于选项创建 restore 子命令
func NewRestoreCommandWithOptions(opts *options.RestoreOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore pod from checkpoint to node",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	// 绑定选项到命令行参数
	opts.AddPFlags(cmd.Flags())

	return cmd
}
