package tools

import (
	"fmt"
	"my-agent/rag"
)

// RAGSearchTool 让Agent能检索知识库
type RAGSearchTool struct {
	engine *rag.Engine
}

func NewRAGSearchTool(engine *rag.Engine) *RAGSearchTool {
	return &RAGSearchTool{engine: engine}
}

func (r *RAGSearchTool) Name() string { return "knowledge_search" }

func (r *RAGSearchTool) Description() string {
	return "在本地知识库中检索相关信息。当用户询问已上传文档中的内容时使用这个工具。适合回答关于特定文档、项目文档、技术资料的问题。"
}

func (r *RAGSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "检索关键词或问题，描述你想从知识库中找到什么",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "返回最相关的几条结果，默认3，最多5",
			},
		},
		"required": []string{"query"},
	}
}

func (r *RAGSearchTool) Execute(args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("缺少 query 参数")
	}

	topK := 3
	if k, ok := args["top_k"].(float64); ok {
		topK = int(k)
		if topK > 5 {
			topK = 5
		}
	}

	result, err := r.engine.Search(query, topK)
	if err != nil {
		return "", fmt.Errorf("知识库检索失败: %w", err)
	}
	return result, nil
}