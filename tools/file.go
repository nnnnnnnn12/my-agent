package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileTool 文件读写工具
// Agent可以用它把生成的报告保存到本地
type FileTool struct {
	OutputDir string // 保存文件的目录
}

func NewFileTool(outputDir string) *FileTool {
	// 确保目录存在
	os.MkdirAll(outputDir, 0755)
	return &FileTool{OutputDir: outputDir}
}

func (f *FileTool) Name() string { return "file_writer" }

func (f *FileTool) Description() string {
	return "将内容保存为本地文件。当用户要求生成报告、保存结果或导出内容时使用。支持保存为.txt或.md格式。"
}

func (f *FileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filename": map[string]interface{}{
				"type":        "string",
				"description": "文件名，例如 report.md 或 result.txt，不需要包含路径",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "要写入文件的内容",
			},
		},
		"required": []string{"filename", "content"},
	}
}

func (f *FileTool) Execute(args map[string]interface{}) (string, error) {
	filename, ok := args["filename"].(string)
	if !ok {
		return "", fmt.Errorf("缺少 filename 参数")
	}
	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("缺少 content 参数")
	}

	// 安全处理：避免路径穿越攻击（../../etc/passwd 这种）
	filename = filepath.Base(filename)

	// 如果没有扩展名，默认加 .md
	if !strings.Contains(filename, ".") {
		filename += ".md"
	}

	// 加时间戳避免覆盖
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	timestamp := time.Now().Format("0102_1504") // 月日_时分
	finalName := fmt.Sprintf("%s_%s%s", base, timestamp, ext)

	fullPath := filepath.Join(f.OutputDir, finalName)

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	return fmt.Sprintf("✅ 文件已保存到: %s（%d 字节）", fullPath, len(content)), nil
}