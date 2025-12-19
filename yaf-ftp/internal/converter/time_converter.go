package converter

import (
	"fmt"
	"strings"
	"time"
)

// TimeConverter 负责将 UTC 时间转换为指定时区
type TimeConverter struct {
	startTimeIndex int
	endTimeIndex   int
	timezone       *time.Location
	initialized    bool
}

// NewTimeConverter 创建新的时间转换器
// headerLine: 表头行，例如 "flowStartMilliseconds|flowEndMilliseconds|..."
func NewTimeConverter(headerLine string, timezone string) (*TimeConverter, error) {
	// 解析时区
	loc, err := parseTimezone(timezone)
	if err != nil {
		return nil, fmt.Errorf("解析时区失败: %w", err)
	}

	// 解析表头，找到时间字段的位置
	fields := strings.Split(headerLine, "|")
	startIndex := -1
	endIndex := -1

	for i, field := range fields {
		field = strings.TrimSpace(field)
		if field == "flowStartMilliseconds" {
			startIndex = i
		} else if field == "flowEndMilliseconds" {
			endIndex = i
		}
	}

	if startIndex == -1 || endIndex == -1 {
		return nil, fmt.Errorf("未找到时间字段: flowStartMilliseconds=%d, flowEndMilliseconds=%d", startIndex, endIndex)
	}

	return &TimeConverter{
		startTimeIndex: startIndex,
		endTimeIndex:   endIndex,
		timezone:       loc,
		initialized:    true,
	}, nil
}

// parseTimezone 解析时区字符串
func parseTimezone(tz string) (*time.Location, error) {
	if tz == "" {
		// 默认使用东八区
		tz = "Asia/Shanghai"
	}

	// 尝试加载时区
	loc, err := time.LoadLocation(tz)
	if err != nil {
		// 如果失败，尝试解析 UTC+8 格式
		if strings.HasPrefix(tz, "UTC+") {
			offset, err := parseUTCOffset(tz)
			if err != nil {
				return nil, fmt.Errorf("无法解析时区: %s", tz)
			}
			return time.FixedZone(tz, offset), nil
		}
		return nil, err
	}

	return loc, nil
}

// parseUTCOffset 解析 UTC+8 格式的时区
func parseUTCOffset(tz string) (int, error) {
	// 简单解析 UTC+8 或 UTC-5 格式
	if strings.HasPrefix(tz, "UTC+") {
		var hours int
		_, err := fmt.Sscanf(tz, "UTC+%d", &hours)
		if err != nil {
			return 0, err
		}
		return hours * 3600, nil
	} else if strings.HasPrefix(tz, "UTC-") {
		var hours int
		_, err := fmt.Sscanf(tz, "UTC-%d", &hours)
		if err != nil {
			return 0, err
		}
		return -hours * 3600, nil
	}
	return 0, fmt.Errorf("无法解析 UTC 偏移: %s", tz)
}

// ConvertLine 转换一行数据中的时间字段
// 如果行不是数据行（如表头、http| 开头的行），直接返回原行
func (tc *TimeConverter) ConvertLine(line string) (string, error) {
	if !tc.initialized {
		return line, nil
	}

	// 跳过空行
	line = strings.TrimSpace(line)
	if line == "" {
		return line, nil
	}

	// 跳过以 http| 开头的 DPI 详细信息行
	if strings.HasPrefix(line, "http|") {
		return line, nil
	}

	// 分割字段
	fields := strings.Split(line, "|")
	if len(fields) <= tc.startTimeIndex || len(fields) <= tc.endTimeIndex {
		// 字段数量不足，可能是格式不对，直接返回
		return line, nil
	}

	// 转换开始时间
	startTime, err := tc.convertTime(fields[tc.startTimeIndex])
	if err != nil {
		// 如果转换失败，保留原值
		// log.Printf("[WARN] 转换开始时间失败: %v", err)
	} else {
		fields[tc.startTimeIndex] = startTime
	}

	// 转换结束时间
	endTime, err := tc.convertTime(fields[tc.endTimeIndex])
	if err != nil {
		// 如果转换失败，保留原值
		// log.Printf("[WARN] 转换结束时间失败: %v", err)
	} else {
		fields[tc.endTimeIndex] = endTime
	}

	// 重新组装行
	return strings.Join(fields, "|"), nil
}

// convertTime 转换单个时间字符串
// 输入格式: "2025-12-01 08:44:51.689" (UTC)
// 输出格式: "2025-12-01 16:44:51.689" (UTC+8)
func (tc *TimeConverter) convertTime(timeStr string) (string, error) {
	timeStr = strings.TrimSpace(timeStr)
	if timeStr == "" {
		return timeStr, nil
	}

	// 解析 UTC 时间
	// 格式: "2006-01-02 15:04:05.000"
	utcTime, err := time.Parse("2006-01-02 15:04:05.000", timeStr)
	if err != nil {
		// 尝试不带毫秒的格式
		utcTime, err = time.Parse("2006-01-02 15:04:05", timeStr)
		if err != nil {
			return timeStr, fmt.Errorf("解析时间失败: %w", err)
		}
	}

	// 转换为目标时区
	localTime := utcTime.In(tc.timezone)

	// 格式化为相同格式
	if strings.Contains(timeStr, ".") {
		// 包含毫秒
		return localTime.Format("2006-01-02 15:04:05.000"), nil
	}
	// 不包含毫秒
	return localTime.Format("2006-01-02 15:04:05"), nil
}

// IsInitialized 检查转换器是否已初始化
func (tc *TimeConverter) IsInitialized() bool {
	return tc.initialized
}

