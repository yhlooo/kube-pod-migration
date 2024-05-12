package containerd

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/cmd/ctr/commands/tasks"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/protobuf"
	"github.com/containerd/containerd/protobuf/proto"
	_ "github.com/containerd/containerd/runtime"
	"github.com/containerd/typeurl/v2"
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	ociimg "github.com/opencontainers/image-spec/specs-go/v1"
	ociruntime "github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/protobuf/types/known/anypb"
	criapis "k8s.io/cri-api/pkg/apis"
	runtimev1 "k8s.io/cri-api/pkg/apis/runtime/v1"

	"github.com/yhlooo/podmig/pkg/utils/randutil"
	"github.com/yhlooo/podmig/pkg/utils/tarutil"
)

// Restore 从 tr 读取 Pod 检查点并还原 Pod
func (h *Manager) Restore(ctx context.Context, tr *tar.Reader) error {
	return (&Restore{
		criClient:        h.criClient,
		containerdClient: h.containerdClient,
		tr:               tr,
	}).Do(ctx)
}

// Restore 从 Pod 检查点还原
type Restore struct {
	criClient        criapis.RuntimeService
	containerdClient *containerd.Client
	tr               *tar.Reader

	srcSandboxInfo               *SandboxInfo
	srcContainerCheckpointImages []images.Image

	sandboxInfo *SandboxInfo
}

// Do 执行从 Pod 检查点还原操作
func (r *Restore) Do(ctx context.Context) error {
	logger := logr.FromContextOrDiscard(ctx)

	// 导入检查点 tar
	logger.Info("importing checkpoint from tar")
	if err := r.importTar(ctx); err != nil {
		return fmt.Errorf("import checkpoint from tar error: %w", err)
	}

	// 还原 Pod 沙盒
	if err := r.restorePodSandbox(ctx); err != nil {
		return fmt.Errorf("restore pod sandbox error: %w", err)
	}

	// 按照创建检查点的逆序恢复容器
	for i := len(r.srcContainerCheckpointImages) - 1; i >= 0; i-- {
		cID, err := r.restoreContainer(ctx, r.srcContainerCheckpointImages[i])
		if err != nil {
			return fmt.Errorf("restore container error: %w", err)
		}
		logger.Info(fmt.Sprintf("restored container: %s", cID))
	}

	return nil
}

// importTar 导入 Pod 检查点 tar
func (r *Restore) importTar(ctx context.Context) error {
	logger := logr.FromContextOrDiscard(ctx)

	for {
		hdr, err := r.tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read checkpoint tar file error: %w", err)
		}

		switch {
		case strings.HasPrefix(hdr.Name, containerCheckpointTarNamePrefix):
			logger.Info(fmt.Sprintf("importing checkpoint image from file %q ...", hdr.Name))
			imgs, err := r.containerdClient.Import(ctx, r.tr)
			if err != nil {
				return fmt.Errorf("import checkpoint image from file %q error: %w", hdr.Name, err)
			}
			for _, imgInfo := range imgs {
				logger.Info(fmt.Sprintf("imported image: %s", imgInfo.Name))
			}
			r.srcContainerCheckpointImages = append(r.srcContainerCheckpointImages, imgs...)
		case hdr.Name == sandboxInfoJSONName:
			logger.Info(fmt.Sprintf("importing sandbox info from file %q ...", hdr.Name))
			r.srcSandboxInfo = &SandboxInfo{}
			if err := tarutil.ReadJSON(r.tr, r.srcSandboxInfo); err != nil {
				return fmt.Errorf("read sandbox config from file %q error: %w", hdr.Name, err)
			}
		case strings.HasPrefix(hdr.Name, kubeletPodDirTarNamePrefix):
			// kubelet Pod 数据目录
			path := strings.TrimPrefix(hdr.Name, kubeletPodDirTarNamePrefix)
			logger.Info(fmt.Sprintf("importing kubelet pod data file %q ...", path))
			info := hdr.FileInfo()

			// 目录
			if info.IsDir() {
				if err := os.MkdirAll(path, info.Mode()); err != nil {
					return fmt.Errorf("mkdir %q error: %w", path, err)
				}
				continue
			}

			// 软链
			if info.Mode()&os.ModeSymlink != 0 {
				if err := os.Symlink(hdr.Linkname, path); err != nil {
					return fmt.Errorf("symlink %q -> %q error: %w", path, hdr.Linkname, err)
				}
				continue
			}

			// 普通文件
			f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
			if err != nil {
				return fmt.Errorf("open file %q error: %w", path, err)
			}
			if _, err := io.Copy(f, r.tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("copy file %q from tar error: %w", path, err)
			}
			_ = f.Close()
		}
	}

	return nil
}

// restorePodSandbox 还原 Pod 沙盒
func (r *Restore) restorePodSandbox(ctx context.Context) error {
	logger := logr.FromContextOrDiscard(ctx)

	sandboxID, err := r.criClient.RunPodSandbox(ctx, r.srcSandboxInfo.Config, "")
	if err != nil {
		return fmt.Errorf("run pod sandbox error: %w", err)
	}
	logger.Info(fmt.Sprintf("restored sandbox: %s", sandboxID))

	// 等待沙盒就绪
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		resp, err := r.criClient.PodSandboxStatus(ctx, sandboxID, false)
		if err != nil {
			return fmt.Errorf("get pod sandbox status error: %w", err)
		}

		if resp.Status.State == runtimev1.PodSandboxState_SANDBOX_READY {
			break
		}
		logger.Info(fmt.Sprintf("wait for pod sandbox ready, current status: %s", resp.Status.State))
	}
	logger.Info("sandbox is ready")

	// 获取沙盒详细信息
	resp, err := r.criClient.PodSandboxStatus(ctx, sandboxID, true)
	if err != nil {
		return fmt.Errorf("get pod sandbox status error: %w", err)
	}
	r.sandboxInfo = &SandboxInfo{}
	if err := json.Unmarshal([]byte(resp.Info["info"]), r.sandboxInfo); err != nil {
		return fmt.Errorf("unmarshal sandbox info from json error: %w", err)
	}
	r.sandboxInfo.ID = sandboxID

	return nil
}

// restoreContainer 还原容器
func (r *Restore) restoreContainer(ctx context.Context, checkpoint images.Image) (string, error) {
	logger := logr.FromContextOrDiscard(ctx)

	// 基于 Pod 沙盒修改容器检查点镜像
	restoreCheckpoint, err := r.convertContainerCheckpointImage(ctx, checkpoint)
	if err != nil {
		return "", fmt.Errorf("convert container checkpoint image %q error: %w", checkpoint.Name, err)
	}

	// 生成还原容器 ID
	// TODO: 暂不清楚 kubelet 如何生成容器 ID ，也不知道是否有其它逻辑依赖该 ID 的生成逻辑，先随机生成
	cID := randutil.NewRand().HexN(64)

	// 还原容器
	restoreCheckpointImage := containerd.NewImage(r.containerdClient, restoreCheckpoint)
	logger.Info(fmt.Sprintf("restoring container from checkpoint image: %s", restoreCheckpoint.Name))
	container, err := r.containerdClient.Restore(
		ctx, cID, restoreCheckpointImage,
		containerd.WithRestoreImage,
		containerd.WithRestoreSpec,
		containerd.WithRestoreRuntime,
		containerd.WithRestoreRW,
	)
	if err != nil {
		return "", err
	}

	// 还原进程
	logger.Info(fmt.Sprintf("restoring task in container from checkpoint image: %s", restoreCheckpoint.Name))
	task, err := tasks.NewTask(
		ctx, r.containerdClient, container, "", nil, false, "",
		[]cio.Opt{},
		containerd.WithTaskCheckpoint(restoreCheckpointImage),
	)
	if err != nil {
		return container.ID(), fmt.Errorf("restore task in container error: %w", err)
	}
	if err := task.Start(ctx); err != nil {
		return container.ID(), fmt.Errorf("start task in container error: %w", err)
	}
	return container.ID(), nil
}

// convertContainerCheckpointImage 转换容器检查点镜像
//
// 因为 Pod 沙盒是重新创建的，进程号、沙盒 ID 等信息发生了变化，因此需要修改检查点中容器配置中对这些信息的引用
func (r *Restore) convertContainerCheckpointImage(
	ctx context.Context,
	checkpointImage images.Image,
) (images.Image, error) {
	logger := logr.FromContextOrDiscard(ctx)

	imgName := checkpointImage.Name

	// 读镜像索引
	imgIndex, err := r.getImageIndex(ctx, checkpointImage.Target)
	if err != nil {
		return images.Image{}, fmt.Errorf("get index from checkpoint image %q error: %w", imgName, err)
	}

	// 读容器配置
	var containerSpecI int
	var containerSpec *ociruntime.Spec
	for i, m := range imgIndex.Manifests {
		if m.MediaType != "application/vnd.containerd.container.checkpoint.config.v1+proto" {
			continue
		}
		containerSpec, err = r.getContainerSpec(ctx, m)
		if err != nil {
			return images.Image{}, fmt.Errorf("get contianer spec from checkpoint image %q error: %w", imgName, err)
		}
		containerSpecI = i
		break
	}
	// 没有容器配置就没有需要转换的
	if containerSpec == nil {
		return checkpointImage, nil
	}

	// 转换容器配置
	r.convertContainerSpec(ctx, containerSpec)

	// 写入新容器配置
	desc, err := r.writeContainerSpec(ctx, containerSpec)
	if err != nil {
		return images.Image{}, fmt.Errorf("write converted container spec for checkpoint image error: %w", err)
	}
	logger.Info(fmt.Sprintf("converted checkpoint image container spec: %s", desc.Digest))
	imgIndex.Manifests[containerSpecI].Size = desc.Size
	imgIndex.Manifests[containerSpecI].Digest = desc.Digest

	// 写入更新的镜像索引
	desc, err = r.writeImageIndex(ctx, imgIndex)
	if err != nil {
		return images.Image{}, fmt.Errorf("write converted checkpoint image index error: %w", err)
	}
	logger.Info(fmt.Sprintf("converted checkpoint image: %s", desc.Digest))

	// 创建转换后的镜像
	newImage := "restore-" + imgName
	logger.Info(fmt.Sprintf("creating converted checkpoint image: %s -> %s", desc.Digest, newImage))
	_ = r.containerdClient.ImageService().Delete(ctx, newImage) // 先删除之前残留的
	return r.containerdClient.ImageService().Create(ctx, images.Image{
		Name:   newImage,
		Target: desc,
	})
}

// convertContainerSpec 转换容器配置
func (r *Restore) convertContainerSpec(ctx context.Context, spec *ociruntime.Spec) {
	// TODO: ...

	if spec.Annotations[containerAnnoSandboxID] == r.srcSandboxInfo.ID {
		spec.Annotations[containerAnnoSandboxID] = r.sandboxInfo.ID
	}
	for i, mount := range spec.Mounts {
		switch mount.Destination {
		case "/etc/hostname", "/etc/resolv.conf", "/dev/shm":
			spec.Mounts[i].Source = strings.ReplaceAll(mount.Source, r.srcSandboxInfo.ID, r.sandboxInfo.ID)
		case "/etc/hosts", "/dev/termination-log", "/var/run/secrets/kubernetes.io/serviceaccount":
		}
	}
	if spec.Linux != nil {
		// 替换 /proc/xxx 路径中的 sandbox pid
		srcProcPath := fmt.Sprintf("/proc/%d", r.srcSandboxInfo.Pid)
		dstProcPath := fmt.Sprintf("/proc/%d", r.sandboxInfo.Pid)
		for i, ns := range spec.Linux.Namespaces {
			if !strings.HasPrefix(ns.Path, srcProcPath) {
				continue
			}
			spec.Linux.Namespaces[i].Path = strings.ReplaceAll(
				ns.Path,
				srcProcPath,
				dstProcPath,
			)
		}
	}
}

// getContainerSpec 从 application/vnd.containerd.container.checkpoint.config.v1+proto 类型的 content 中读取容器配置信息
func (r *Restore) getContainerSpec(ctx context.Context, desc ociimg.Descriptor) (*ociruntime.Spec, error) {
	// 检查类型
	if desc.MediaType != mediaTypeContainerSpec {
		return nil, fmt.Errorf("unexpected media type %q, must be %q", desc.MediaType, mediaTypeContainerSpec)
	}

	// 读内容
	raw, err := content.ReadBlob(ctx, r.containerdClient.ContentStore(), desc)
	if err != nil {
		return nil, err
	}

	// 反序列化
	anyObj := &anypb.Any{}
	if err := proto.Unmarshal(raw, anyObj); err != nil {
		return nil, fmt.Errorf("unmarshal from proto error: %w", err)
	}
	var spec ociruntime.Spec
	if err := typeurl.UnmarshalTo(anyObj, &spec); err != nil {
		return nil, fmt.Errorf("unmarsal from any error: %w", err)
	}

	return &spec, nil
}

// writeContainerSpec 将 application/vnd.containerd.container.checkpoint.config.v1+proto 类型的容器配置信息写入 content
func (r *Restore) writeContainerSpec(ctx context.Context, spec *ociruntime.Spec) (ociimg.Descriptor, error) {
	// 序列化
	anyObj, err := protobuf.MarshalAnyToProto(spec)
	if err != nil {
		return ociimg.Descriptor{}, fmt.Errorf("marshal to any error: %w", err)
	}
	raw, err := proto.Marshal(anyObj)
	if err != nil {
		return ociimg.Descriptor{}, fmt.Errorf("marshal to proto error: %w", err)
	}

	// 写 content
	dgst := digest.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(raw)))
	desc := ociimg.Descriptor{
		MediaType: mediaTypeContainerSpec,
		Digest:    dgst,
		Size:      int64(len(raw)),
	}
	return desc, content.WriteBlob(ctx, r.containerdClient.ContentStore(), string(dgst), bytes.NewReader(raw), desc)
}

// getImageIndex 从 application/vnd.oci.image.index.v1+json 类型的 content 中读取镜像索引信息
func (r *Restore) getImageIndex(ctx context.Context, desc ociimg.Descriptor) (*ociimg.Index, error) {
	// 检查类型
	if desc.MediaType != ociimg.MediaTypeImageIndex {
		return nil, fmt.Errorf("unexpected media type %q, must be %q", desc.MediaType, ociimg.MediaTypeImageIndex)
	}

	// 读内容
	raw, err := content.ReadBlob(ctx, r.containerdClient.ContentStore(), desc)
	if err != nil {
		return nil, err
	}

	// 反序列化
	var index ociimg.Index
	return &index, json.Unmarshal(raw, &index)
}

// writeImageIndex 将 application/vnd.oci.image.index.v1+json 类型的镜像索引信息写入 content
func (r *Restore) writeImageIndex(ctx context.Context, index *ociimg.Index) (ociimg.Descriptor, error) {
	// 序列化
	raw, err := json.Marshal(index)
	if err != nil {
		return ociimg.Descriptor{}, fmt.Errorf("marshal to json error: %w", err)
	}

	// 确定标签
	labels := map[string]string{}
	for i, m := range index.Manifests {
		// 垃圾回收相关的标签
		labels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i)] = m.Digest.String()
	}

	// 写 content
	dgst := digest.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(raw)))
	desc := ociimg.Descriptor{
		MediaType: ociimg.MediaTypeImageIndex,
		Digest:    dgst,
		Size:      int64(len(raw)),
	}

	return desc, content.WriteBlob(
		ctx,
		r.containerdClient.ContentStore(),
		string(dgst), // 这里的 ref 是用于唯一标识 content 写传输会话的，跟镜像的 ref 没有关系，所以用 digest
		bytes.NewReader(raw),
		desc,
		content.WithLabels(labels),
	)
}
