package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// SearchTool 搜索工具，使用DuckDuckGo免费API（不需要Key）
type SearchTool struct{}

func NewSearchTool() *SearchTool {
	return &SearchTool{}
}

func (s *SearchTool) Name() string {
	return "web_search"
}

func (s *SearchTool) Description() string {
	return "搜索互联网获取实时信息。当用户询问最新新闻、实时数据、或你不确定的事实时，使用这个工具。"
}

func (s *SearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "搜索关键词，用英文效果更好",
			},
		},
		"required": []string{"query"},
	}
}

func (s *SearchTool) Execute(args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("缺少 query 参数")
	}

	// 使用DuckDuckGo的即时回答API（完全免费，无需Key）
	apiURL := fmt.Sprintf(
		"https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		url.QueryEscape(query),
	)

	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("搜索请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	// 尝试提取摘要信息
	var parts []string

	// 主摘要
	if abstract, ok := result["Abstract"].(string); ok && abstract != "" {
		parts = append(parts, abstract)
	}

	// 即时回答
	if answer, ok := result["Answer"].(string); ok && answer != "" {
		parts = append(parts, "直接回答: "+answer)
	}

	// 相关话题
	if topics, ok := result["RelatedTopics"].([]interface{}); ok {
		count := 0
		for _, t := range topics {
			if count >= 3 {
				break
			}
			if topic, ok := t.(map[string]interface{}); ok {
				if text, ok := topic["Text"].(string); ok && text != "" {
					parts = append(parts, "- "+text)
					count++
				}
			}
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf("搜索了 '%s'，但没有找到直接结果。建议换个关键词或用英文搜索。", query), nil
	}

	return strings.Join(parts, "\n"), nil
}