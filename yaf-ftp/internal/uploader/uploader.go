package uploader

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// Uploader 负责定时扫描目录并上传文件到 FTP
type Uploader struct {
	ftpHost          string
	ftpPort          int
	ftpUser          string
	ftpPass          string
	ftpDir           string
	dataDir          string
	uploadIntervalSec int
	stopChan         chan struct{}
	doneChan         chan struct{}
}

// NewUploader 创建新的 Uploader
func NewUploader(ftpHost string, ftpPort int, ftpUser, ftpPass, ftpDir, dataDir string, uploadIntervalSec int) *Uploader {
	return &Uploader{
		ftpHost:          ftpHost,
		ftpPort:          ftpPort,
		ftpUser:          ftpUser,
		ftpPass:          ftpPass,
		ftpDir:           ftpDir,
		dataDir:          dataDir,
		uploadIntervalSec: uploadIntervalSec,
		stopChan:         make(chan struct{}),
		doneChan:         make(chan struct{}),
	}
}

// Start 启动上传器，在后台 goroutine 中运行
func (u *Uploader) Start() {
	go u.run()
}

// Stop 停止上传器
func (u *Uploader) Stop() {
	close(u.stopChan)
	<-u.doneChan
}

// run 主循环：定时扫描并上传
func (u *Uploader) run() {
	defer close(u.doneChan)

	ticker := time.NewTicker(time.Duration(u.uploadIntervalSec) * time.Second)
	defer ticker.Stop()

	// 立即执行一次
	u.scanAndUpload()

	for {
		select {
		case <-ticker.C:
			u.scanAndUpload()
		case <-u.stopChan:
			return
		}
	}
}

// scanAndUpload 扫描数据目录并上传所有 .csv.gz 文件
func (u *Uploader) scanAndUpload() {
	entries, err := os.ReadDir(u.dataDir)
	if err != nil {
		log.Printf("[ERROR] 扫描数据目录失败: %v", err)
		return
	}

	var filesToUpload []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".csv.gz") {
			filesToUpload = append(filesToUpload, entry.Name())
		}
	}

	if len(filesToUpload) == 0 {
		return
	}

	log.Printf("[INFO] 发现 %d 个待上传文件", len(filesToUpload))

	for _, filename := range filesToUpload {
		filePath := filepath.Join(u.dataDir, filename)
		if err := u.uploadFile(filePath, filename); err != nil {
			log.Printf("[ERROR] FTP 上传失败: %s -> %v", filename, err)
			// 继续处理下一个文件，不删除失败的文件
			continue
		}

		// 上传成功，删除本地文件
		if err := os.Remove(filePath); err != nil {
			log.Printf("[ERROR] 删除本地文件失败: %s -> %v", filename, err)
		} else {
			log.Printf("[INFO] FTP 上传成功并删除本地文件: %s", filename)
		}
	}
}

// uploadFile 上传单个文件到 FTP
func (u *Uploader) uploadFile(localPath, filename string) error {
	// 连接 FTP 服务器
	addr := fmt.Sprintf("%s:%d", u.ftpHost, u.ftpPort)
	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return fmt.Errorf("连接 FTP 服务器失败: %w", err)
	}
	defer conn.Quit()

	// 登录
	if err := conn.Login(u.ftpUser, u.ftpPass); err != nil {
		return fmt.Errorf("FTP 登录失败: %w", err)
	}

	// 确保远程目录存在
	if err := u.ensureRemoteDir(conn, u.ftpDir); err != nil {
		return fmt.Errorf("创建远程目录失败: %w", err)
	}

	// 构建远程文件路径
	remotePath := u.ftpDir + "/" + filename
	if strings.HasSuffix(u.ftpDir, "/") {
		remotePath = u.ftpDir + filename
	}

	// 检查 FTP 服务器上是否已存在该文件（避免重复上传）
	entries, err := conn.List(u.ftpDir)
	if err == nil {
		for _, entry := range entries {
			if entry.Name == filename {
				// 文件已存在，跳过上传
				log.Printf("[INFO] FTP 服务器上已存在文件，跳过上传: %s", filename)
				return nil
			}
		}
	}

	// 打开本地文件
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer file.Close()

	// 上传文件
	if err := conn.Stor(remotePath, file); err != nil {
		return fmt.Errorf("上传文件失败: %w", err)
	}

	return nil
}

// ensureRemoteDir 确保远程目录存在
func (u *Uploader) ensureRemoteDir(conn *ftp.ServerConn, dir string) error {
	// 尝试切换到目录，如果失败则创建
	if err := conn.ChangeDir(dir); err != nil {
		// 目录不存在，尝试创建
		parts := strings.Split(strings.Trim(dir, "/"), "/")
		currentPath := ""
		for _, part := range parts {
			if part == "" {
				continue
			}
			if currentPath == "" {
				currentPath = "/" + part
			} else {
				currentPath = currentPath + "/" + part
			}
			if err := conn.ChangeDir(currentPath); err != nil {
				if err := conn.MakeDir(currentPath); err != nil {
					// 可能目录已存在（并发创建），忽略错误
					log.Printf("[WARN] 创建远程目录可能失败（可能已存在）: %s", currentPath)
				}
			}
		}
		// 最后再尝试切换一次
		if err := conn.ChangeDir(dir); err != nil {
			return fmt.Errorf("无法切换到远程目录: %w", err)
		}
	}
	return nil
}

