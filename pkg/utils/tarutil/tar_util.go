package tarutil

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// WriteJSON 将 JSON 写入 tar
func WriteJSON(tw *tar.Writer, name string, mode int64, v interface{}) error {
	// 序列化
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal data to json error: %w", err)
	}

	// 写头
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: mode,
		Size: int64(len(data)),
	}); err != nil {
		return fmt.Errorf("write tar header error: %w", err)
	}

	// 写内容
	_, err = tw.Write(data)
	return err
}

// ReadJSON 从 tar 读取 JSON
func ReadJSON(tr *tar.Reader, v interface{}) error {
	dataRaw, err := io.ReadAll(tr)
	if err != nil {
		return err
	}
	return json.Unmarshal(dataRaw, v)
}

// CopyIn 将文件拷贝到 tar
func CopyIn(tw *tar.Writer, name string, mode int64, srcPath string) error {
	// 打开源文件
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

	// 写头
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: mode,
		Size: fstat.Size(),
	}); err != nil {
		return fmt.Errorf("write tar header error: %w", err)
	}

	// 拷贝内容
	_, err = io.Copy(tw, f)
	return err
}
