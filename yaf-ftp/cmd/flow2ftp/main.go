package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/yaf-ftp/flow2ftp/internal/config"
	"github.com/yaf-ftp/flow2ftp/internal/converter"
	"github.com/yaf-ftp/flow2ftp/internal/uploader"
	"github.com/yaf-ftp/flow2ftp/internal/writer"
)

var (
	configPath = flag.String("config", "", "YAF 配置文件路径（yaf.init）")
	dataDir    = flag.String("data-dir", "", "本地缓存目录，存放滚动生成的压缩文件")
	logLevel   = flag.String("log-level", "info", "日志级别: debug|info|warn|error")
)

func main() {
	flag.Parse()

	// 验证必需参数
	if *configPath == "" {
		log.Fatal("[ERROR] -config 参数是必需的")
	}
	if *dataDir == "" {
		log.Fatal("[ERROR] -data-dir 参数是必需的")
	}

	// 加载配置
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("[ERROR] 加载配置失败: %v", err)
	}
	log.Printf("[INFO] 配置加载成功: FTP=%s:%d, 滚动间隔=%ds, 滚动大小=%dMB, 上传间隔=%ds",
		cfg.FTPHost, cfg.FTPPort, cfg.RotateIntervalSec, cfg.RotateSizeMB, cfg.UploadIntervalSec)

	// 确保数据目录存在
	if err := config.EnsureDataDir(*dataDir); err != nil {
		log.Fatalf("[ERROR] 数据目录准备失败: %v", err)
	}
	log.Printf("[INFO] 数据目录已就绪: %s", *dataDir)

	// 创建 Writer
	w := writer.NewWriter(*dataDir, cfg.FilePrefix, cfg.RotateIntervalSec, cfg.RotateSizeMB)

	// 创建 Uploader
	up := uploader.NewUploader(
		cfg.FTPHost,
		cfg.FTPPort,
		cfg.FTPUser,
		cfg.FTPPass,
		cfg.FTPDir,
		*dataDir,
		cfg.UploadIntervalSec,
	)

	// 启动上传器
	up.Start()
	log.Printf("[INFO] FTP 上传器已启动，上传间隔: %ds", cfg.UploadIntervalSec)

	// 设置信号处理
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动从 stdin 读取并写入的 goroutine
	done := make(chan error, 1)
	go func() {
		done <- processStdin(ctx, w, cfg.Timezone)
	}()

	// 等待信号或完成
	select {
	case sig := <-sigChan:
		log.Printf("[INFO] 收到信号: %v，开始优雅关闭...", sig)
		cancel()
	case err := <-done:
		if err != nil {
			log.Printf("[ERROR] 处理 stdin 时出错: %v", err)
		}
	}

	// 关闭 writer（确保当前文件被正确关闭和重命名）
	if err := w.Close(); err != nil {
		log.Printf("[ERROR] 关闭 writer 失败: %v", err)
	} else {
		log.Printf("[INFO] Writer 已关闭")
	}

	// 停止上传器
	up.Stop()
	log.Printf("[INFO] 程序退出")
}

// processStdin 从标准输入读取数据并写入文件
func processStdin(ctx context.Context, w *writer.Writer, timezone string) error {
	scanner := bufio.NewScanner(os.Stdin)
	lineCount := 0
	var timeConverter *converter.TimeConverter
	headerProcessed := false

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("读取 stdin 失败: %w", err)
				}
				// EOF
				log.Printf("[INFO] 从 stdin 读取完成，共处理 %d 行", lineCount)
				return nil
			}

			line := scanner.Text()
			if len(line) == 0 {
				continue
			}

			// 处理表头行：初始化时间转换器
			if !headerProcessed {
				// 检查是否是表头行（包含 flowStartMilliseconds）
				if strings.Contains(line, "flowStartMilliseconds") {
					var err error
					timeConverter, err = converter.NewTimeConverter(line, timezone)
					if err != nil {
						log.Printf("[WARN] 初始化时间转换器失败: %v，将不进行时区转换", err)
						timeConverter = nil
					} else {
						log.Printf("[INFO] 时间转换器已初始化，目标时区: %s", timezone)
					}
					headerProcessed = true
				}
				// 表头行直接写入，不转换
				if err := w.WriteLine(line); err != nil {
					log.Printf("[ERROR] 写入数据失败: %v", err)
					continue
				}
				continue
			}

			// 处理数据行：转换时间字段
			outputLine := line
			if timeConverter != nil && timeConverter.IsInitialized() {
				converted, err := timeConverter.ConvertLine(line)
				if err != nil {
					// 转换失败，使用原行
					log.Printf("[WARN] 转换时间失败: %v，使用原始数据", err)
				} else {
					outputLine = converted
				}
			}

			if err := w.WriteLine(outputLine); err != nil {
				log.Printf("[ERROR] 写入数据失败: %v", err)
				// 继续处理，不中断
				continue
			}

			lineCount++
			if lineCount%10000 == 0 {
				log.Printf("[INFO] 已处理 %d 行数据", lineCount)
			}
		}
	}
}

