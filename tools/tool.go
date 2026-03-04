package tools

// Tool 是所有工具必须实现的接口
// 这是Go interface的经典用法：定义行为，不关心实现
type Tool interface {
	// Name 返回工具名称，LLM通过这个名字来调用工具
	Name() string

	// Description 描述工具的功能
	// 这段描述会发给LLM，LLM根据描述决定要不要用这个工具
	// 描述写得好不好，直接影响LLM的判断准确性！
	Description() string

	// Parameters 描述工具需要哪些参数（JSON Schema格式）
	// LLM会按照这个格式来填写参数
	Parameters() map[string]interface{}

	// Execute 执行工具，返回结果
	Execute(args map[string]interface{}) (string, error)
}

// ToolDefinition 是发给LLM的工具描述格式（OpenAI标准格式）
type ToolDefinition struct {
	Type     string         `json:"type"`
	Function FunctionDetail `json:"function"`
}

type FunctionDetail struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToDefinition 把Tool转换成LLM能理解的格式
func ToDefinition(t Tool) ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: FunctionDetail{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		},
	}
}