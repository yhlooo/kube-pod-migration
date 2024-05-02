package options

// NewDefaultOptions 创建一个默认运行选项
func NewDefaultOptions() Options {
	return Options{
		Global:     NewDefaultGlobalOptions(),
		Checkpoint: NewDefaultCheckpointOptions(),
		Restore:    NewDefaultRestoreOptions(),
	}
}

// Options pcrctl 运行选项
type Options struct {
	// 全局选项
	Global GlobalOptions `json:"global,omitempty" yaml:"global,omitempty"`
	// checkpoint 子命令选项
	Checkpoint CheckpointOptions `json:"checkpoint,omitempty" yaml:"checkpoint,omitempty"`
	// restore 子命令选项
	Restore RestoreOptions `json:"restore,omitempty" yaml:"restore,omitempty"`
}
