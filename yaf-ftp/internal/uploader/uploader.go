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
	ftpHost           string
	ftpPort           int
	ftpUser           string
	ftpPass           string
	ftpDir            string
	dataDir           string
	uploadIntervalSec int
	stopChan          chan struct{}
	doneChan          chan struct{}
}

// NewUploader 创建新的 Uploader
func NewUploader(ftpHost string, ftpPort int, ftpUser, ftpPass, ftpDir, dataDir string, uploadIntervalSec int) *Uploader {
	return &Uploader{
		ftpHost:           ftpHost,
		ftpPort:           ftpPort,
		ftpUser:           ftpUser,
		ftpPass:           ftpPass,
		ftpDir:            ftpDir,
		dataDir:           dataDir,
		uploadIntervalSec: uploadIntervalSec,
		stopChan:          make(chan struct{}),
		doneChan:          make(chan struct{}),
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
	if err := u.cleanupRemoteTempFiles(); err != nil {
		log.Printf("[WARN] 清理远端临时文件失败: %v", err)
	}

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
	log.Printf("[INFO] 准备上传文件: %s", filename)

	// 获取本地文件大小
	localInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("获取本地文件信息失败: %w", err)
	}
	localSize := localInfo.Size()

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

	// 构建远程文件路径（最终文件 + 临时文件）
	remotePath := u.ftpDir + "/" + filename
	if strings.HasSuffix(u.ftpDir, "/") {
		remotePath = u.ftpDir + filename
	}
	tempName := filename + ".tmp"
	remoteTempPath := u.ftpDir + "/" + tempName
	if strings.HasSuffix(u.ftpDir, "/") {
		remoteTempPath = u.ftpDir + tempName
	}

	// 检查 FTP 服务器上是否已存在最终文件（避免重复上传）
	if remoteSize, err := conn.FileSize(remotePath); err == nil {
		if remoteSize == localSize {
			log.Printf("[INFO] 远端已存在同名文件且大小一致，跳过上传: %s (size=%d)", filename, localSize)
			return nil
		}
		log.Printf("[WARN] 远端已存在同名文件但大小不一致，将尝试覆盖: %s (local=%d, remote=%d)", filename, localSize, remoteSize)
		if err := conn.Delete(remotePath); err != nil {
			log.Printf("[WARN] 删除远端旧文件失败（将继续尝试上传临时文件）: %s -> %v", remotePath, err)
		}
	}

	// 如果存在残留临时文件，先尝试删除（避免改名冲突）
	if remoteTempSize, err := conn.FileSize(remoteTempPath); err == nil {
		log.Printf("[WARN] 发现远端残留临时文件，尝试删除: %s (size=%d)", remoteTempPath, remoteTempSize)
		if err := conn.Delete(remoteTempPath); err != nil {
			log.Printf("[WARN] 删除远端临时文件失败（将继续尝试覆盖上传）: %s -> %v", remoteTempPath, err)
		}
	}

	// 打开本地文件
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer file.Close()

	// 上传文件到临时路径
	log.Printf("[INFO] 开始上传临时文件: %s -> %s (size=%d)", filename, remoteTempPath, localSize)
	if err := conn.Stor(remoteTempPath, file); err != nil {
		return fmt.Errorf("上传临时文件失败: %w", err)
	}

	// 上传完成后校验大小
	remoteTempSize, err := conn.FileSize(remoteTempPath)
	if err != nil {
		return fmt.Errorf("获取远端临时文件大小失败: %w", err)
	}
	if remoteTempSize != localSize {
		return fmt.Errorf("远端临时文件大小不一致: local=%d, remote=%d", localSize, remoteTempSize)
	}
	log.Printf("[INFO] 远端临时文件大小校验通过: %s (size=%d)", remoteTempPath, remoteTempSize)

	// 重命名为最终文件
	log.Printf("[INFO] 重命名远端临时文件: %s -> %s", remoteTempPath, remotePath)
	if err := conn.Rename(remoteTempPath, remotePath); err != nil {
		return fmt.Errorf("重命名远端文件失败: %w", err)
	}
	log.Printf("[INFO] 上传完成: %s (size=%d)", filename, localSize)

	return nil
}

// cleanupRemoteTempFiles 清理远端残留临时文件（.tmp）
func (u *Uploader) cleanupRemoteTempFiles() error {
	addr := fmt.Sprintf("%s:%d", u.ftpHost, u.ftpPort)
	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return fmt.Errorf("连接 FTP 服务器失败: %w", err)
	}
	defer conn.Quit()

	if err := conn.Login(u.ftpUser, u.ftpPass); err != nil {
		return fmt.Errorf("FTP 登录失败: %w", err)
	}

	if err := u.ensureRemoteDir(conn, u.ftpDir); err != nil {
		return fmt.Errorf("创建远程目录失败: %w", err)
	}

	entries, err := conn.List(u.ftpDir)
	if err != nil {
		return fmt.Errorf("列出远端目录失败: %w", err)
	}

	cleaned := 0
	for _, entry := range entries {
		if entry.Type != ftp.EntryTypeFile {
			continue
		}
		name := entry.Name
		if !strings.HasSuffix(name, ".tmp") {
			continue
		}
		remotePath := u.ftpDir + "/" + name
		if strings.HasSuffix(u.ftpDir, "/") {
			remotePath = u.ftpDir + name
		}
		if err := conn.Delete(remotePath); err != nil {
			log.Printf("[WARN] 删除远端临时文件失败: %s -> %v", remotePath, err)
			continue
		}
		cleaned++
		log.Printf("[INFO] 已清理远端临时文件: %s", remotePath)
	}

	if cleaned > 0 {
		log.Printf("[INFO] 远端临时文件清理完成: %d", cleaned)
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
