package pcrctl

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"github.com/yhlooo/podmig/pkg/commands/pcrctl/options"
	podcrcommon "github.com/yhlooo/podmig/pkg/podcr/common"
	podcrcontianerd "github.com/yhlooo/podmig/pkg/podcr/containerd"
)

// NewRestoreCommandWithOptions 基于选项创建 restore 子命令
func NewRestoreCommandWithOptions(opts *options.RestoreOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore pod from checkpoint to node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch opts.ContainerRuntime {
			case "containerd":
			default:
				return fmt.Errorf("unsupported container runtime: %s", opts.ContainerRuntime)
			}

			ctx := cmd.Context()
			logger := logr.FromContextOrDiscard(ctx)

			checkpointTar := args[0]
			file, err := os.Open(checkpointTar)
			if err != nil {
				return fmt.Errorf("open checkpoint tar file %q error: %w", checkpointTar, err)
			}
			defer func() { _ = file.Close() }()
			gzipR, err := gzip.NewReader(file)
			if err != nil {
				return fmt.Errorf("open gzip reader for checkpoint tar file %q error: %w", checkpointTar, err)
			}
			defer func() { _ = gzipR.Close() }()
			tr := tar.NewReader(gzipR)

			var mgr podcrcommon.PodCRManager
			switch opts.ContainerRuntime {
			case "containerd":
				mgr, err = podcrcontianerd.New(opts.ContainerRuntimeEndpoint, "")
			}
			if err != nil {
				return fmt.Errorf("create pod restore manager error: %w", err)
			}

			if err := mgr.Restore(ctx, tr); err != nil {
				return err
			}
			logger.Info("restored")

			return nil
		},
	}

	// 绑定选项到命令行参数
	opts.AddPFlags(cmd.Flags())

	return cmd
}
