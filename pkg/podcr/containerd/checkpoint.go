package containerd

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images/archive"
	"github.com/containerd/typeurl/v2"
	"github.com/go-logr/logr"
	ociruntime "github.com/opencontainers/runtime-spec/specs-go"
	criapis "k8s.io/cri-api/pkg/apis"
	runtimev1 "k8s.io/cri-api/pkg/apis/runtime/v1"

	"github.com/yhlooo/podmig/pkg/utils/tarutil"
)

// Checkpoint 建立 Pod 检查点，并导出到 tw
func (h *Manager) Checkpoint(ctx context.Context, checkpointID, namespace, name string, tw *tar.Writer) error {
	tmpdir, err := os.MkdirTemp(h.tmpdir, "pod-checkpoint-")
	if err != nil {
		return fmt.Errorf("make temp dir error: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpdir)
	}()

	return (&Checkpoint{
		tmpdir:                 tmpdir,
		criClient:              h.criClient,
		containerdClient:       h.containerdClient,
		retainCheckpointImages: h.retainCheckpointImages,
		checkpointID:           checkpointID,
		namespace:              namespace,
		name:                   name,
		tw:                     tw,
	}).Do(ctx)
}

// Checkpoint 建立 Pod 检查点
type Checkpoint struct {
	tmpdir                 string
	criClient              criapis.RuntimeService
	containerdClient       *containerd.Client
	retainCheckpointImages bool
	checkpointID           string
	namespace              string
	name                   string
	tw                     *tar.Writer

	sandboxInfo *SandboxInfo
	containers  []*runtimev1.Container
}

// Do 执行建立 Pod 检查点操作
func (c *Checkpoint) Do(ctx context.Context) error {
	podKey := c.namespace + "/" + c.name
	logger := logr.FromContextOrDiscard(ctx).WithValues("pod", podKey)
	ctx = logr.NewContext(ctx, logger)

	// 导出 Pod 沙盒
	if err := c.exportPodSandbox(ctx); err != nil {
		return fmt.Errorf("export pod sandbox %q error: %w", podKey, err)
	}
	logger.Info(fmt.Sprintf("pod sandbox: %s", c.sandboxInfo.ID[:13]))

	// 导出容器基础信息
	if err := c.exportContainersInfo(ctx); err != nil {
		return fmt.Errorf("export containers info error: %w", err)
	}
	ids := make([]string, len(c.containers))
	for i, container := range c.containers {
		ids[i] = fmt.Sprintf("%s(%s)", container.Id[:13], container.Metadata.GetName())
	}
	logger.Info(fmt.Sprintf("containers: %v", ids))

	// 按容器创建顺序反向创建检查点
	for i := len(c.containers) - 1; i >= 0; i-- {
		cName := c.containers[i].Metadata.GetName()
		// 创建容器检查点
		logger.Info(fmt.Sprintf("checkpoint container %q", cName))
		checkpointImage, err := c.checkpointContainer(ctx, c.containers[i])
		if err != nil {
			return fmt.Errorf("checkpoint container %q for pod %q error: %w", cName, podKey, err)
		}
		logger.Info(fmt.Sprintf("checkpoint: %s", checkpointImage.Name()))

		// 将容器检查点镜像导出到文件
		if err := c.exportContainerCheckpoint(ctx, cName, checkpointImage.Name()); err != nil {
			return fmt.Errorf("export container %q checkpoint for pod %q error: %w", cName, podKey, err)
		}
	}

	// 导出 kubelet Pod 目录
	if err := c.exportKubeletPodDir(ctx); err != nil {
		return fmt.Errorf("export kubelet pod dir error: %w", err)
	}

	return nil
}

// exportPodSandbox 导出 Pod 沙盒
func (c *Checkpoint) exportPodSandbox(ctx context.Context) error {
	podKey := c.namespace + "/" + c.name

	sandboxes, err := c.criClient.ListPodSandbox(ctx, &runtimev1.PodSandboxFilter{
		LabelSelector: map[string]string{
			"io.kubernetes.pod.name":      c.name,
			"io.kubernetes.pod.namespace": c.namespace,
		},
	})
	if err != nil {
		return err
	}

	if len(sandboxes) == 0 {
		return fmt.Errorf("pod sandbox %q not found", podKey)
	} else if len(sandboxes) > 1 {
		ids := make([]string, len(sandboxes))
		for i, s := range sandboxes {
			ids[i] = s.Id[:13]
		}
		return fmt.Errorf("the pod %q has more than one sandbox: %v", podKey, ids)
	}
	baseInfo := sandboxes[0]

	// 获取 Pod 沙盒详细配置
	resp, err := c.criClient.PodSandboxStatus(ctx, baseInfo.Id, true)
	if err != nil {
		return err
	}
	c.sandboxInfo = &SandboxInfo{}
	if err := json.Unmarshal([]byte(resp.Info["info"]), c.sandboxInfo); err != nil {
		return fmt.Errorf("unmarshal sandbox info from json error: %w", err)
	}

	// 写 Pod 沙盒配置
	c.sandboxInfo.ID = baseInfo.Id
	if err := tarutil.WriteJSON(c.tw, sandboxInfoJSONName, 0644, c.sandboxInfo); err != nil {
		return fmt.Errorf("write sandbox config to tar error: %w", err)
	}

	return nil
}

// exportKubeletPodDir 导出 kubelet Pod 目录
func (c *Checkpoint) exportKubeletPodDir(ctx context.Context) error {
	logger := logr.FromContextOrDiscard(ctx)

	// 获取 kubelet Pod 导出目录
	kubeletPodDir, err := c.getKubeletPodDir(ctx)
	if err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("exporting kubelet pod directory: %s", kubeletPodDir))

	// 将 Pod 数据目录全部打包
	return filepath.Walk(kubeletPodDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 获取软链目标
		link := path
		isSymlink := info.Mode()&os.ModeSymlink != 0
		if isSymlink {
			link, err = os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read link %q error: %w", path, err)
			}
		}

		// 写文件头
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return fmt.Errorf("get tar header %q error: %w", path, err)
		}
		hdr.Name = kubeletPodDirTarNamePrefix + path
		if err := c.tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header %q error: %w", path, err)
		}
		if info.IsDir() || isSymlink {
			return nil
		}

		// 写文件内容
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open file %q error: %w", path, err)
		}
		defer func() { _ = f.Close() }()
		if _, err := io.Copy(c.tw, f); err != nil {
			return fmt.Errorf("copy file %q to tar error: %w", path, err)
		}
		return nil
	})
}

// exportContainersInfo 导出容器基础信息
func (c *Checkpoint) exportContainersInfo(ctx context.Context) error {
	var err error
	c.containers, err = c.criClient.ListContainers(ctx, &runtimev1.ContainerFilter{
		PodSandboxId: c.sandboxInfo.ID,
	})
	if err != nil {
		return err
	}

	// 按容器创建时间排序
	sort.Slice(c.containers, func(i, j int) bool {
		return c.containers[i].CreatedAt < c.containers[j].CreatedAt
	})

	return nil
}

// checkpointContainer 建立容器检查点
func (c *Checkpoint) checkpointContainer(
	ctx context.Context,
	containerInfo *runtimev1.Container,
) (containerd.Image, error) {
	containerID := containerInfo.Id
	logger := logr.FromContextOrDiscard(ctx)

	// 获取容器和 task 信息
	container, err := c.containerdClient.LoadContainer(ctx, containerID)
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
		c.getContainerCheckpointImageName(containerInfo.Metadata.GetName()),
		containerd.WithCheckpointRuntime,
		containerd.WithCheckpointRW,
		containerd.WithCheckpointTask,
	)
	if err != nil {
		return nil, fmt.Errorf("checkpoint container %q error: %w", containerID, err)
	}

	return checkpoint, nil
}

// exportContainerCheckpoint 导出容器检查点镜像
func (c *Checkpoint) exportContainerCheckpoint(ctx context.Context, containerName, checkpointImageName string) error {
	logger := logr.FromContextOrDiscard(ctx)

	exportName := c.getContainerCheckpointImageTarName(containerName)

	// 创建临时导出文件
	tmpfile := filepath.Join(c.tmpdir, exportName)
	logger.Info(fmt.Sprintf("exporting checkpoint %q to tmp file %q", checkpointImageName, tmpfile))
	w, err := os.Create(tmpfile)
	if err != nil {
		return fmt.Errorf("create export file %q error: %w", tmpfile, err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			logger.Error(err, fmt.Sprintf("close export file %q error", tmpfile))
			return
		}
		_ = os.Remove(tmpfile)
	}()

	// 导出镜像
	imageService := c.containerdClient.ImageService()
	if err := c.containerdClient.Export(ctx, w, archive.WithImage(imageService, checkpointImageName)); err != nil {
		return fmt.Errorf("export checkpoint %q to file %q error: %w", checkpointImageName, tmpfile, err)
	}

	// 将导出镜像文件写入到 tar
	if err := tarutil.CopyIn(c.tw, exportName, 0644, tmpfile); err != nil {
		return fmt.Errorf("copy checkpoint %q exported file %q to tar error: %w", checkpointImageName, tmpfile, err)
	}

	// 删除检查点镜像
	if !c.retainCheckpointImages {
		if err := c.containerdClient.ImageService().Delete(ctx, checkpointImageName); err != nil {
			return fmt.Errorf("delete checkpoint image %q error: %w", checkpointImageName, err)
		}
	}

	return nil
}

// getKubeletPodDir 获取 kubelet Pod 数据目录
func (c *Checkpoint) getKubeletPodDir(ctx context.Context) (string, error) {
	// 默认目录
	defaultKubeletPodDir := filepath.Join(kubeletPodsDir, c.sandboxInfo.Config.Metadata.Uid)

	if len(c.containers) == 0 {
		return defaultKubeletPodDir, nil
	}

	container, err := c.containerdClient.ContainerService().Get(ctx, c.containers[0].Id)
	if err != nil {
		return "", fmt.Errorf("get container %q info error: %w", c.containers[0].Id, err)
	}
	cSpec := &ociruntime.Spec{}
	if err := typeurl.UnmarshalTo(container.Spec, cSpec); err != nil {
		return "", fmt.Errorf("unmarshal container %q info error: %w", c.containers[0].Id, err)
	}
	for _, mount := range cSpec.Mounts {
		if mount.Destination == "/etc/hosts" {
			// 使用 hosts 文件挂载路径推断 Pod 目录
			return filepath.Dir(mount.Source), nil
		}
	}

	return defaultKubeletPodDir, nil
}

// getContainerCheckpointImageName 获取容器检查点镜像名
func (c *Checkpoint) getContainerCheckpointImageName(containerName string) string {
	return fmt.Sprintf("checkpoint-%s:%s_%s_%s", c.checkpointID, c.namespace, c.name, containerName)
}

// getContainerCheckpointImageName 获取容器检查点镜像导出文件名
func (c *Checkpoint) getContainerCheckpointImageTarName(containerName string) string {
	return containerCheckpointTarNamePrefix + containerName + ".tar"
}
