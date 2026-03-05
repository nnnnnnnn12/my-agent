package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// TavilyTool 使用 Tavily API 进行搜索
// Tavily 专为 AI Agent 设计，返回结构化的搜索结果
type TavilyTool struct {
	APIKey string
}

func NewTavilyTool() *TavilyTool {
	apiKey := os.Getenv("TAVILY_API_KEY")
	if apiKey == "" {
		panic("请设置环境变量 TAVILY_API_KEY")
	}
	return &TavilyTool{APIKey: apiKey}
}

func (t *TavilyTool) Name() string { return "web_search" }

func (t *TavilyTool) Description() string {
	return "搜索互联网获取实时信息。当用户询问最新新闻、当前数据、或需要验证事实时使用。"
}

func (t *TavilyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "搜索关键词，描述清楚想查什么",
			},
		},
		"required": []string{"query"},
	}
}

// tavilyRequest Tavily API 请求体
type tavilyRequest struct {
	APIKey         string `json:"api_key"`
	Query          string `json:"query"`
	SearchDepth    string `json:"search_depth"`     // "basic" 或 "advanced"
	MaxResults     int    `json:"max_results"`
	IncludeAnswer  bool   `json:"include_answer"`   // 让Tavily直接给一个摘要答案
}

// tavilyResponse Tavily API 响应体
type tavilyResponse struct {
	Answer  string `json:"answer"` // Tavily生成的直接答案
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"` // 网页摘要
		Score   float64 `json:"score"`  // 相关性分数
	} `json:"results"`
}

func (t *TavilyTool) Execute(args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("缺少 query 参数")
	}

	// 构造请求
	reqBody := tavilyRequest{
		APIKey:        t.APIKey,
		Query:         query,
		SearchDepth:   "basic", // basic省额度，advanced质量更高
		MaxResults:    3,       // 返回3条结果够用了
		IncludeAnswer: true,    // 让Tavily直接生成摘要
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化失败: %w", err)
	}

	resp, err := http.Post(
		"https://api.tavily.com/search",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", fmt.Errorf("搜索请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	var result tavilyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w，原始: %s", err, string(body))
	}

	// 组装返回内容
	var parts []string

	// 优先用Tavily的直接答案
	if result.Answer != "" {
		parts = append(parts, "📝 摘要: "+result.Answer)
	}

	// 附上来源列表
	if len(result.Results) > 0 {
		parts = append(parts, "\n📚 来源:")
		for i, r := range result.Results {
			// 截断过长的content
			content := r.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			parts = append(parts, fmt.Sprintf("%d. [%s](%s)\n   %s", i+1, r.Title, r.URL, content))
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf("搜索 '%s' 未找到相关结果", query), nil
	}

	return strings.Join(parts, "\n"), nil
}