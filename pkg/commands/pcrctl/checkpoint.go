package pcrctl

import (
	"github.com/spf13/cobra"

	"github.com/yhlooo/podmig/pkg/commands/pcrctl/options"
)

// NewCheckpointCommandWithOptions 基于选项创建 checkpoint 子命令
func NewCheckpointCommandWithOptions(opts *options.CheckpointOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Checkpoint a running pod on node",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	// 绑定选项到命令行参数
	opts.AddPFlags(cmd.Flags())

	return cmd
}
