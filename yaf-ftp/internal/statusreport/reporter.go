package statusreport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yaf-ftp/flow2ftp/internal/config"
)

// Reporter 负责聚合流量统计并定期上报
type Reporter struct {
	cfg       config.StatusReportConfig
	client    *http.Client
	startTime time.Time

	totalPkts  atomic.Int64
	totalBytes atomic.Int64

	mu            sync.Mutex
	lastPkts      int64
	lastBytes     int64
	lastTimestamp time.Time
	uuid          string
}

// NewReporter 创建 Reporter（未启用时返回 nil, nil）
func NewReporter(cfg config.StatusReportConfig) (*Reporter, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	uuid := strings.TrimSpace(cfg.UUID)
	if uuid == "" {
		h, _ := os.Hostname()
		uuid = h
	}

	return &Reporter{
		cfg:           cfg,
		client:        &http.Client{Timeout: 10 * time.Second},
		startTime:     time.Now(),
		lastTimestamp: time.Now(),
		uuid:          uuid,
	}, nil
}

// Add 累加一次包/字节统计
func (r *Reporter) Add(pkts, bytes int64) {
	if r == nil {
		return
	}
	r.totalPkts.Add(pkts)
	r.totalBytes.Add(bytes)
}

// Run 启动周期上报
func (r *Reporter) Run(ctxDone <-chan struct{}) {
	if r == nil {
		return
	}
	ticker := time.NewTicker(time.Duration(r.cfg.IntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctxDone:
			return
		case <-ticker.C:
			r.reportOnce()
		}
	}
}

func (r *Reporter) reportOnce() {
	now := time.Now()
	totalPkts := r.totalPkts.Load()
	totalBytes := r.totalBytes.Load()

	r.mu.Lock()
	deltaPkts := totalPkts - r.lastPkts
	deltaBytes := totalBytes - r.lastBytes
	elapsedWindow := now.Sub(r.lastTimestamp).Seconds()
	if elapsedWindow <= 0 {
		elapsedWindow = 1
	}
	r.lastPkts = totalPkts
	r.lastBytes = totalBytes
	r.lastTimestamp = now
	r.mu.Unlock()

	runSecs := now.Sub(r.startTime).Seconds()
	if runSecs <= 0 {
		runSecs = 1
	}

	payload := map[string]interface{}{
		"curRcvPkts":    deltaPkts,
		"curRcvBytes":   deltaBytes,
		"curPkts":       deltaPkts,
		"curBytes":      deltaBytes,
		"curAvgRcvPps":  float64(deltaPkts) / elapsedWindow,
		"curAvgRcvBps":  float64(deltaBytes) / elapsedWindow,
		"curAvgPps":     float64(deltaPkts) / elapsedWindow,
		"curAvgBps":     float64(deltaBytes) / elapsedWindow,
		"uuid":          r.uuid,
		"runSecs":       int64(runSecs),
		"totalRcvPkts":  totalPkts,
		"totalRcvBytes": totalBytes,
		"totalPkts":     totalPkts,
		"totalBytes":    totalBytes,
		"totalAvgRcvPps": func() float64 {
			return float64(totalPkts) / runSecs
		}(),
		"totalAvgRcvBps": func() float64 {
			return float64(totalBytes) / runSecs
		}(),
		"totalAvgPps": func() float64 {
			return float64(totalPkts) / runSecs
		}(),
		"totalAvgBps": func() float64 {
			return float64(totalBytes) / runSecs
		}(),
	}

	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ERROR] 状态上报序列化失败: %v", err)
		return
	}

	// 本地落盘（可选，无论 HTTP 是否成功都落盘）
	if r.cfg.FilePath != "" {
		if err := r.appendToFile(b); err != nil {
			log.Printf("[WARN] 状态上报落盘失败: %v", err)
		}
	}

	// HTTP 上报（失败不影响落盘）
	req, err := http.NewRequest(http.MethodPost, r.cfg.URL, bytes.NewReader(b))
	if err != nil {
		log.Printf("[ERROR] 状态上报请求创建失败: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		log.Printf("[ERROR] 状态上报失败: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("[WARN] 状态上报返回非 2xx: %s", resp.Status)
	}
}

// appendToFile 追加写入文件并做简单大小轮转
func (r *Reporter) appendToFile(data []byte) error {
	path := r.cfg.FilePath
	if path == "" {
		return nil
	}
	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	// 简单轮转：超过阈值则按顺序备份
	if err := r.rotateIfNeeded(path); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	ts := time.Now().Format(time.RFC3339)
	if _, err := f.WriteString(ts + " " + string(data) + "\n"); err != nil {
		return err
	}
	return nil
}

func (r *Reporter) rotateIfNeeded(path string) error {
	maxBytes := int64(r.cfg.FileMaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < maxBytes {
		return nil
	}

	// 不删除历史，只寻找下一个未占用的后缀，追加保存
	for i := 1; ; i++ {
		next := fmt.Sprintf("%s.%03d", path, i)
		if _, err := os.Stat(next); os.IsNotExist(err) {
			return os.Rename(path, next)
		}
		// 若存在则继续递增，直到找到空闲名
		if i > 100000 {
			return fmt.Errorf("轮转文件过多，放弃重命名: %s", path)
		}
	}
}
