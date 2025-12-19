package writer

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer 负责从 stdin 读取数据并滚动写入 gzip 文件
type Writer struct {
	dataDir           string
	filePrefix        string
	rotateIntervalSec int
	rotateSizeMB      int

	currentFile   *os.File
	currentGzip   *gzip.Writer
	currentPath   string
	startTime     time.Time
	writtenBytes  int64
	fileIndex     int
	mu            sync.Mutex
	closed        bool
}

// NewWriter 创建新的 Writer
func NewWriter(dataDir, filePrefix string, rotateIntervalSec, rotateSizeMB int) *Writer {
	return &Writer{
		dataDir:           dataDir,
		filePrefix:        filePrefix,
		rotateIntervalSec: rotateIntervalSec,
		rotateSizeMB:      rotateSizeMB,
		fileIndex:         0,
	}
}

// WriteLine 写入一行数据
func (w *Writer) WriteLine(line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return fmt.Errorf("writer 已关闭")
	}

	// 如果当前文件不存在，创建新文件
	if w.currentFile == nil {
		if err := w.rotateFile(); err != nil {
			return fmt.Errorf("创建新文件失败: %w", err)
		}
	}

	// 写入数据（包括换行符）
	data := line + "\n"
	n, err := w.currentGzip.Write([]byte(data))
	if err != nil {
		return fmt.Errorf("写入数据失败: %w", err)
	}

	w.writtenBytes += int64(len(data))

	// 检查是否需要滚动
	if w.shouldRotate() {
		if err := w.rotateFile(); err != nil {
			return fmt.Errorf("滚动文件失败: %w", err)
		}
	}

	// 刷新 gzip writer 确保数据写入
	if err := w.currentGzip.Flush(); err != nil {
		return fmt.Errorf("刷新数据失败: %w", err)
	}

	_ = n // 避免未使用变量警告
	return nil
}

// shouldRotate 检查是否应该滚动文件
func (w *Writer) shouldRotate() bool {
	// 检查时间间隔
	if time.Since(w.startTime) >= time.Duration(w.rotateIntervalSec)*time.Second {
		return true
	}

	// 检查文件大小（原始字节数，不是压缩后）
	if w.writtenBytes >= int64(w.rotateSizeMB)*1024*1024 {
		return true
	}

	return false
}

// rotateFile 滚动到新文件
func (w *Writer) rotateFile() error {
	// 关闭当前文件
	if w.currentGzip != nil {
		w.currentGzip.Close()
		w.currentGzip = nil
	}
	if w.currentFile != nil {
		w.currentFile.Close()
		w.currentFile = nil

		// 将 .part 文件重命名为 .csv.gz
		if w.currentPath != "" {
			finalPath := w.currentPath[:len(w.currentPath)-5] + ".csv.gz"
			if err := os.Rename(w.currentPath, finalPath); err != nil {
				return fmt.Errorf("重命名文件失败: %w", err)
			}
		}
	}

	// 生成新文件名
	now := time.Now()
	timestamp := now.Format("20060102_150405")
	filename := fmt.Sprintf("%s%s_%03d.part", w.filePrefix, timestamp, w.fileIndex)
	w.currentPath = filepath.Join(w.dataDir, filename)

	// 创建新文件
	file, err := os.Create(w.currentPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}

	gzipWriter := gzip.NewWriter(file)

	w.currentFile = file
	w.currentGzip = gzipWriter
	w.startTime = now
	w.writtenBytes = 0
	w.fileIndex++

	return nil
}

// Close 关闭 writer，确保当前文件被正确关闭和重命名
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true

	if w.currentGzip != nil {
		if err := w.currentGzip.Close(); err != nil {
			return fmt.Errorf("关闭 gzip writer 失败: %w", err)
		}
		w.currentGzip = nil
	}

	if w.currentFile != nil {
		if err := w.currentFile.Close(); err != nil {
			return fmt.Errorf("关闭文件失败: %w", err)
		}
		w.currentFile = nil

		// 将 .part 文件重命名为 .csv.gz
		if w.currentPath != "" {
			finalPath := w.currentPath[:len(w.currentPath)-5] + ".csv.gz"
			if err := os.Rename(w.currentPath, finalPath); err != nil {
				return fmt.Errorf("重命名文件失败: %w", err)
			}
		}
	}

	return nil
}

// GetDataDir 返回数据目录路径
func (w *Writer) GetDataDir() string {
	return w.dataDir
}

