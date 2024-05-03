package pcrctl

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images/archive"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/trace/noop"
	criapis "k8s.io/cri-api/pkg/apis"
	runtimev1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/kubernetes/pkg/kubelet/cri/remote"

	"github.com/yhlooo/podmig/pkg/commands/pcrctl/options"
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
			exportFile := opts.ExportFile
			if exportFile == "" {
				exportFile = fmt.Sprintf(
					"%s_%s_checkpoint_%s.tar.gz",
					podNS, podName,
					randutil.NewRand().LowerAlphaNumN(8),
				)
			}

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

			switch opts.ContainerRuntime {
			case "containerd":
				if err := checkpointInContainerd(
					ctx, tw, exportFile,
					opts.ContainerRuntimeEndpoint,
					podNS, podName,
				); err != nil {
					return err
				}
			}

			logger.Info(fmt.Sprintf("exported pod checkpoint to file: %s", exportFile))
			return nil
		},
	}

	// 绑定选项到命令行参数
	opts.AddPFlags(cmd.Flags())

	return cmd
}

// checkpointInContainerd 基于 containerd 容器运行时创建 Pod 检查点
func checkpointInContainerd(ctx context.Context, tw *tar.Writer, exportFile, endpoint, ns, name string) error {
	podKey := ns + "/" + name
	logger := logr.FromContextOrDiscard(ctx).WithValues("pod", podKey)

	// 创建 CRI 和 containerd 客户端
	criClient, err := getCRIClient(endpoint)
	if err != nil {
		return fmt.Errorf("create cri client error: %w", err)
	}
	containerdClient, err := getContainerdClient(endpoint)
	if err != nil {
		return fmt.Errorf("create containerd client error: %w", err)
	}

	// 获取 Pod 沙箱信息
	sandbox, err := getPodSandbox(ctx, criClient, ns, name)
	if err != nil {
		return fmt.Errorf("get pod %q sandbox error: %w", podKey, err)
	}
	sandboxIDShort := sandbox.Id[:13]
	logger.Info(fmt.Sprintf("pod sandbox: %s", sandboxIDShort))

	// 获取 Pod 对应容器信息
	containers, err := getPodContainers(ctx, criClient, sandbox.Id)
	if err != nil {
		return fmt.Errorf("get containers for sandbox %q error: %w", sandboxIDShort, err)
	}
	ids := make([]string, len(containers))
	for i, container := range containers {
		ids[i] = fmt.Sprintf("%s(%s)", container.Id[:13], container.Metadata.GetName())
	}
	logger.Info(fmt.Sprintf("containers: %v", ids))

	// 按容器创建顺序反向创建检查点
	for i := len(containers) - 1; i >= 0; i-- {
		cName := containers[i].Metadata.GetName()
		logger.Info(fmt.Sprintf("start checkpoint container %q for pod", cName))
		checkpoint, err := checkpointContainerdContainer(
			ctx, containerdClient, containers[i].Id,
			fmt.Sprintf("%s/%s/%s:%s", ns, name, cName, randutil.NewRand().LowerAlphaNumN(8)),
		)
		if err != nil {
			return fmt.Errorf("checkpoint container %q for pod %q error: %w", cName, podKey, err)
		}
		logger.Info(fmt.Sprintf("checkpoint: %s", checkpoint.Name()))

		// 因为写入 tar 文件需要知道大小，而导出检查点镜像不能提前知道大小，所以用一个文件中转
		tmpFile := exportFile + ".tmp" + strconv.Itoa(i)
		logger.Info(fmt.Sprintf("exporting checkpoint %q to tmp file %q", checkpoint.Name(), tmpFile))
		if err := exportContainerdCheckpoint(ctx, containerdClient, checkpoint.Name(), tmpFile); err != nil {
			return fmt.Errorf("export checkpoint %q to file %q error: %w", checkpoint.Name(), tmpFile, err)
		}
		logger.Info(fmt.Sprintf("copying checkpoint tmp file %q to tar %q", tmpFile, exportFile))
		if err := copyToTar(tw, tmpFile, fmt.Sprintf("container_%s.tar", cName)); err != nil {
			return fmt.Errorf("copy checkpoint %q exported file %q to tar error: %w", checkpoint.Name(), tmpFile, err)
		}
		if err := os.Remove(tmpFile); err != nil {
			logger.Error(err, fmt.Sprintf("remove checkpoint exported tmp file %q error", tmpFile))
		}
	}

	return nil
}

// getPodSandbox 获取 CRI Pod 沙盒信息
func getPodSandbox(ctx context.Context, c criapis.RuntimeService, ns, name string) (*runtimev1.PodSandbox, error) {
	podKey := ns + "/" + name

	sandboxes, err := c.ListPodSandbox(ctx, &runtimev1.PodSandboxFilter{
		LabelSelector: map[string]string{
			"io.kubernetes.pod.name":      name,
			"io.kubernetes.pod.namespace": ns,
		},
	})
	if err != nil {
		return nil, err
	}

	if len(sandboxes) == 0 {
		return nil, fmt.Errorf("pod sandbox %q not found", podKey)
	} else if len(sandboxes) > 1 {
		ids := make([]string, len(sandboxes))
		for i, s := range sandboxes {
			ids[i] = s.Id[:13]
		}
		return nil, fmt.Errorf("the pod %q has more than one sandbox: %v", podKey, ids)
	}
	return sandboxes[0], nil
}

// getPodContainers 获取 CRI Pod 容器信息
func getPodContainers(ctx context.Context, c criapis.RuntimeService, sandboxID string) ([]*runtimev1.Container, error) {
	containers, err := c.ListContainers(ctx, &runtimev1.ContainerFilter{
		PodSandboxId: sandboxID,
	})
	if err != nil {
		return nil, err
	}

	// 按容器创建时间排序
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].CreatedAt < containers[j].CreatedAt
	})

	return containers, nil
}

// checkpointContainerdContainer 基于 containerd 创建容器快照
func checkpointContainerdContainer(
	ctx context.Context,
	c *containerd.Client,
	containerID, checkpointImageName string,
) (containerd.Image, error) {
	logger := logr.FromContextOrDiscard(ctx).WithValues("container", containerID)

	// 获取容器和 task 信息
	container, err := c.LoadContainer(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("load container %q error: %w", containerID, err)
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get task for container %q error: %w", containerID, err)
	}

	// 先暂停进程
	if err := task.Pause(ctx); err != nil {
		return nil, fmt.Errorf("pause task for container %q error: %w", containerID, err)
	}
	defer func() {
		// 还原进程
		// TODO: 应该通过选项决定是否应该还原并保持运行
		if err = task.Resume(ctx); err != nil {
			logger.Error(err, fmt.Sprintf("resume task for container %q error", containerID))
		}
	}()

	// 建立检查点
	checkpoint, err := container.Checkpoint(
		ctx,
		checkpointImageName,
		containerd.WithCheckpointRuntime,
		containerd.WithCheckpointRW,
		containerd.WithCheckpointTask,
	)
	if err != nil {
		return nil, fmt.Errorf("checkpoint container %q error: %w", containerID, err)
	}

	return checkpoint, nil
}

// exportContainerdCheckpoint 将 containerd 快照导出到文件
func exportContainerdCheckpoint(
	ctx context.Context,
	c *containerd.Client,
	checkpointImageName, exportPath string,
) error {
	logger := logr.FromContextOrDiscard(ctx).WithValues("checkpoint", checkpointImageName)

	w, err := os.Create(exportPath)
	if err != nil {
		return fmt.Errorf("create export file %q error: %w", exportPath, err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			logger.Error(err, fmt.Sprintf("close export file %q error", exportPath))
		}
	}()

	return c.Export(ctx, w, archive.WithImage(c.ImageService(), checkpointImageName))
}

// copyToTar 将文件拷贝到 tar 中
func copyToTar(tw *tar.Writer, srcPath, name string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open file %q error: %w", srcPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	fstat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("get file %q info error: %w", srcPath, err)
	}

	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0644,
		Size: fstat.Size(),
	}); err != nil {
		return fmt.Errorf("write tar header error: %w", err)
	}

	_, err = io.Copy(tw, f)
	return err
}

// getCRIClient 获取 CRI 客户端
func getCRIClient(endpoint string) (criapis.RuntimeService, error) {
	tp := noop.NewTracerProvider()
	timeout := 2 * time.Second
	return remote.NewRemoteRuntimeService(endpoint, timeout, tp)
}

// getContainerdClient 获取 containerd 客户端
func getContainerdClient(endpoint string) (*containerd.Client, error) {
	opts := []containerd.ClientOpt{
		containerd.WithDefaultNamespace("k8s.io"),
	}
	endpoint = strings.TrimPrefix(endpoint, "unix://")
	return containerd.New(endpoint, opts...)
}
