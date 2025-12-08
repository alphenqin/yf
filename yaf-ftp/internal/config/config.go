package config

import (
	"fmt"
	"os"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// containsFlow2FTP 检查字符串中是否包含 flow2ftp 配置块
func containsFlow2FTP(content string) bool {
	// 转换为小写进行匹配，更宽松的检测
	contentLower := strings.ToLower(content)
	// 检查是否包含 flow2ftp 关键字（允许各种格式）
	if strings.Contains(contentLower, "flow2ftp") {
		return true
	}
	return false
}

// Flow2FTPConfig 包含 flow2ftp 的所有配置项
type Flow2FTPConfig struct {
	FTPHost           string
	FTPPort           int
	FTPUser           string
	FTPPass           string
	FTPDir            string
	RotateIntervalSec int
	RotateSizeMB      int
	FilePrefix        string
	UploadIntervalSec int
	Timezone          string // 时区，例如 "Asia/Shanghai" 或 "UTC+8"，默认为 "Asia/Shanghai"
}

// LoadConfig 从 yaf.init 文件中解析 flow2ftp 配置块
func LoadConfig(configPath string) (*Flow2FTPConfig, error) {
	// 检查文件是否存在
	if _, err := os.Stat(configPath); err != nil {
		return nil, fmt.Errorf("配置文件不存在: %w", err)
	}

	// 使用 gopher-lua 解析 Lua 配置文件
	L := lua.NewState()
	defer L.Close()

	// 读取文件内容用于调试
	fileContent, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 执行 Lua 文件
	if err := L.DoFile(configPath); err != nil {
		return nil, fmt.Errorf("执行 Lua 文件失败: %w", err)
	}

	// 尝试多种方式获取 flow2ftp
	// 方式1: 直接获取全局变量
	tbl := L.GetGlobal("flow2ftp")
	
	// 方式2: 如果方式1失败，尝试通过 Lua 代码获取
	if tbl == nil || tbl == lua.LNil {
		// 尝试执行 Lua 代码获取
		if err := L.DoString("return flow2ftp"); err == nil {
			if ret := L.Get(-1); ret != nil && ret != lua.LNil {
				tbl = ret
				L.Pop(1)
			}
		}
	}

	// 如果还是找不到，列出所有检测到的全局变量
	if tbl == nil || tbl == lua.LNil {
		var foundGlobals []string
		testGlobals := []string{"input", "output", "applabel", "filter", "flow2ftp"}
		for _, name := range testGlobals {
			if val := L.GetGlobal(name); val != nil && val != lua.LNil {
				foundGlobals = append(foundGlobals, name)
			}
		}
		
		// 检查配置文件中是否包含 flow2ftp 关键字
		contentStr := string(fileContent)
		hasFlow2FTP := containsFlow2FTP(contentStr)
		
		// 尝试直接执行 flow2ftp 相关的 Lua 代码来诊断
		diagnosticInfo := ""
		if hasFlow2FTP {
			// 尝试执行一段 Lua 代码来检查 flow2ftp
			if err := L.DoString("if flow2ftp then return 'flow2ftp exists' else return 'flow2ftp is nil' end"); err == nil {
				if ret := L.Get(-1); ret != nil && ret != lua.LNil {
					diagnosticInfo = fmt.Sprintf(" (Lua 诊断: %s)", ret.String())
					L.Pop(1)
				}
			}
			
			// 查找 flow2ftp 所在的行用于调试
			lines := strings.Split(contentStr, "\n")
			for i, line := range lines {
				if strings.Contains(strings.ToLower(line), "flow2ftp") {
					// 显示 flow2ftp 所在行及前后各2行
					start := i - 2
					if start < 0 {
						start = 0
					}
					end := i + 3
					if end > len(lines) {
						end = len(lines)
					}
					context := strings.Join(lines[start:end], "\n")
					diagnosticInfo += fmt.Sprintf(" (找到 flow2ftp 在第 %d 行附近:\n%s)", i+1, context)
					break
				}
			}
		}
		
		errMsg := fmt.Sprintf("未找到 flow2ftp 配置块。检测到的全局变量: %v", foundGlobals)
		if hasFlow2FTP {
			errMsg += fmt.Sprintf(" (配置文件中包含 flow2ftp 关键字，但 Lua 解析时未注册为全局变量%s)", diagnosticInfo)
		} else {
			// 如果没找到，也尝试显示配置文件的前几行用于调试
			lines := strings.Split(contentStr, "\n")
			preview := ""
			if len(lines) > 0 {
				previewLines := lines
				if len(previewLines) > 10 {
					previewLines = previewLines[:10]
				}
				preview = fmt.Sprintf(" (配置文件前 %d 行:\n%s)", len(previewLines), strings.Join(previewLines, "\n"))
			}
			errMsg += fmt.Sprintf(" (配置文件中未找到 flow2ftp 关键字，请检查配置文件%s)", preview)
		}
		return nil, fmt.Errorf(errMsg)
	}

	flowTbl, ok := tbl.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("flow2ftp 配置块格式错误: 期望表类型，实际类型: %T", tbl)
	}

	// 从 flowTbl 中读取配置项
	cfg := &Flow2FTPConfig{}

	// 读取字符串配置
	if val := flowTbl.RawGetString("ftp_host"); val != nil && val != lua.LNil {
		cfg.FTPHost = val.String()
	}
	if val := flowTbl.RawGetString("ftp_user"); val != nil && val != lua.LNil {
		cfg.FTPUser = val.String()
	}
	if val := flowTbl.RawGetString("ftp_pass"); val != nil && val != lua.LNil {
		cfg.FTPPass = val.String()
	}
	if val := flowTbl.RawGetString("ftp_dir"); val != nil && val != lua.LNil {
		cfg.FTPDir = val.String()
	}
	if val := flowTbl.RawGetString("file_prefix"); val != nil && val != lua.LNil {
		cfg.FilePrefix = val.String()
	}

	// 读取数字配置
	if val := flowTbl.RawGetString("ftp_port"); val != nil && val != lua.LNil {
		if num, ok := val.(lua.LNumber); ok {
			cfg.FTPPort = int(num)
		}
	}
	if val := flowTbl.RawGetString("rotate_interval_sec"); val != nil && val != lua.LNil {
		if num, ok := val.(lua.LNumber); ok {
			cfg.RotateIntervalSec = int(num)
		}
	}
	if val := flowTbl.RawGetString("rotate_size_mb"); val != nil && val != lua.LNil {
		if num, ok := val.(lua.LNumber); ok {
			cfg.RotateSizeMB = int(num)
		}
	}
	if val := flowTbl.RawGetString("upload_interval_sec"); val != nil && val != lua.LNil {
		if num, ok := val.(lua.LNumber); ok {
			cfg.UploadIntervalSec = int(num)
		}
	}
	if val := flowTbl.RawGetString("timezone"); val != nil && val != lua.LNil {
		cfg.Timezone = val.String()
	}

	// 验证配置
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return cfg, nil
}


// validateConfig 验证配置的有效性
func validateConfig(cfg *Flow2FTPConfig) error {
	if cfg.FTPHost == "" {
		return fmt.Errorf("ftp_host 不能为空")
	}
	if cfg.FTPUser == "" {
		return fmt.Errorf("ftp_user 不能为空")
	}
	if cfg.FTPPass == "" {
		return fmt.Errorf("ftp_pass 不能为空")
	}
	if cfg.RotateIntervalSec < 1 {
		return fmt.Errorf("rotate_interval_sec 必须 >= 1")
	}
	if cfg.RotateSizeMB < 1 {
		return fmt.Errorf("rotate_size_mb 必须 >= 1")
	}
	if cfg.UploadIntervalSec < 1 {
		return fmt.Errorf("upload_interval_sec 必须 >= 1")
	}
	if cfg.FilePrefix == "" {
		cfg.FilePrefix = "flows_"
	}
	if cfg.FTPPort == 0 {
		cfg.FTPPort = 21
	}
	if cfg.FTPDir == "" {
		cfg.FTPDir = "/"
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Shanghai" // 默认东八区
	}

	return nil
}

// EnsureDataDir 确保数据目录存在
func EnsureDataDir(dataDir string) error {
	info, err := os.Stat(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(dataDir, 0755); err != nil {
				return fmt.Errorf("创建数据目录失败: %w", err)
			}
			return nil
		}
		return fmt.Errorf("检查数据目录失败: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("数据目录路径不是目录: %s", dataDir)
	}
	return nil
}

