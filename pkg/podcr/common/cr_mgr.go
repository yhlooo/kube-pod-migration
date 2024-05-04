package common

import (
	"archive/tar"
	"context"
)

// PodCRManager Pod Checkpoint/Restore manager
type PodCRManager interface {
	// Checkpoint 建立 Pod 检查点，并导出到 tw
	Checkpoint(ctx context.Context, checkpointID, namespace, name string, tw *tar.Writer) error
	// Restore 从 tr 读取 Pod 检查点并还原 Pod
	Restore(ctx context.Context, tr *tar.Reader) error
}
