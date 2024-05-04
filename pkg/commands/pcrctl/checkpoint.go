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
	"github.com/yhlooo/podmig/pkg/utils/randutil"
)

// NewCheckpointCommandWithOptions 基于选项创建 checkpoint 子命令
func NewCheckpointCommandWithOptions(opts *options.CheckpointOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoint POD",
		Short: "Checkpoint a running pod on node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch opts.ContainerRuntime {
			case "containerd":
			default:
				return fmt.Errorf("unsupported container runtime: %s", opts.ContainerRuntime)
			}

			ctx := cmd.Context()
			logger := logr.FromContextOrDiscard(ctx)

			podName := args[0]
			podNS := opts.Namespace
			checkpointID := randutil.NewRand().LowerAlphaNumN(8)
			exportFile := opts.ExportFile
			if exportFile == "" {
				exportFile = fmt.Sprintf("%s_%s_checkpoint_%s.tar.gz", podNS, podName, checkpointID)
			}

			// 准备临时文件目录
			tmpdir := exportFile + ".tmp"
			if err := os.Mkdir(tmpdir, 0755); err != nil {
				return fmt.Errorf("make temp dir %q error: %w", tmpdir, err)
			}
			defer func() { _ = os.RemoveAll(tmpdir) }()

			// 打开导出 tar 文件
			file, err := os.Create(exportFile)
			if err != nil {
				return fmt.Errorf("failed to create export file %q: %w", exportFile, err)
			}
			gzipW := gzip.NewWriter(file)
			tw := tar.NewWriter(gzipW)
			defer func() {
				if err := tw.Close(); err != nil {
					logger.Error(err, "close tar writer error")
				}
				if err := gzipW.Close(); err != nil {
					logger.Error(err, "close gzip writer error")
				}
				if err := file.Close(); err != nil {
					logger.Error(err, "close export file writer error")
				}
			}()

			// 准备检查点管理器
			var mgr podcrcommon.PodCRManager
			switch opts.ContainerRuntime {
			case "containerd":
				mgr, err = podcrcontianerd.New(opts.ContainerRuntimeEndpoint, tmpdir, opts.RetainCheckpointImages)
			}
			if err != nil {
				return fmt.Errorf("create pod checkpoint manager error: %w", err)
			}

			// 建立检查点
			if err := mgr.Checkpoint(ctx, checkpointID, podNS, podName, tw); err != nil {
				return err
			}

			logger.Info(fmt.Sprintf("exported pod checkpoint to file: %s", exportFile))
			return nil
		},
	}

	// 绑定选项到命令行参数
	opts.AddPFlags(cmd.Flags())

	return cmd
}
