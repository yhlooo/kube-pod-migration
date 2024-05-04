package containerd

import (
	"archive/tar"
	"context"
)

// Restore 从 tr 读取 Pod 检查点并还原 Pod
func (h *Manager) Restore(ctx context.Context, tr *tar.Reader) error {
	//TODO implement me
	panic("implement me")
}
