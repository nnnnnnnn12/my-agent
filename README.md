# GoAgent

> 用 Go 语言从零实现的 ReAct AI Agent 框架，支持工具调用、并发执行和 HTTP API 服务化部署。

## 项目简介

GoAgent 是一个基于大语言模型（LLM）的智能 Agent 框架，核心实现了 **ReAct（Reasoning + Acting）** 模式：Agent 能够自主分解任务、选择合适工具、循环推理，直到完成复杂目标。

区别于 Python 生态的 LangChain 等框架，GoAgent 完全用 Go 原生实现，充分利用 goroutine 并发优势，适合对性能和工程质量有要求的生产场景。

```
用户输入任务
    ↓
LLM 思考：需要哪些工具？
    ↓
并发调用多个工具（goroutine）
    ↓
LLM 观察结果，继续推理
    ↓
循环直到任务完成
    ↓
返回最终答案
```

## 技术亮点

- **ReAct 循环**：自主实现 Thought → Action → Observation 推理链，无依赖第三方 Agent 框架
- **并发工具执行**：使用 `sync.WaitGroup` + goroutine 并发调用多工具，相比串行执行性能提升显著
- **会话管理**：基于 `sync.RWMutex` 的线程安全会话存储，支持多用户并发请求
- **可扩展 Tool 接口**：统一 `Tool` interface，新增工具只需实现 4 个方法
- **HTTP API**：基于 Gin 框架，支持跨域、会话隔离、健康检查

## 项目结构

```
my-agent/
├── main.go              # 入口，支持 CLI / Server 双模式
├── go.mod
├── llm/
│   └── openai.go        # LLM 客户端，兼容 OpenAI / DeepSeek API
├── agent/
│   ├── react.go         # ReAct 核心循环 + 并发工具执行
│   ├── agent.go         # 基础 ToolAgent
│   └── memory.go        # 对话历史管理
├── tools/
│   ├── tool.go          # Tool 接口定义
│   ├── calculator.go    # 数学计算工具
│   ├── search.go        # 网络搜索工具
│   └── file.go          # 文件读写工具
└── api/
    └── server.go        # HTTP API 服务（Gin）
```

## 快速开始

### 环境要求

- Go 1.21+
- DeepSeek API Key（[免费注册](https://platform.deepseek.com)，费用极低）

### 安装运行

```bash
# 克隆项目
git clone https://github.com/你的用户名/my-agent.git
cd my-agent

# 安装依赖
go mod tidy

# 设置 API Key
export DEEPSEEK_API_KEY=your_key_here   # Linux/Mac
set DEEPSEEK_API_KEY=your_key_here      # Windows

# 命令行模式
go run .

# HTTP 服务模式
go run . server
```

### 命令行演示

```
🤖 My Agent - ReAct Agent
你: 帮我计算 (1024 + 2048) * 3 的结果，并保存到文件

[步骤 1]
🔧 需要调用 1 个工具
   ⚡ [goroutine 1] 调用 calculator(expression="(1024+2048)*3")
   👁  [goroutine 1] 结果: (1024+2048)*3 = 9216

[步骤 2]
🔧 需要调用 1 个工具
   ⚡ [goroutine 1] 调用 file_writer(filename="result.md", ...)
   👁  [goroutine 1] 结果: ✅ 文件已保存到: ./output/result_0305_1423.md

✅ 任务完成（共 3 步）
🤖 GoAgent: 计算结果为 9216，已保存到文件。
```

### HTTP API 使用

```bash
# 健康检查
curl http://localhost:8080/health

# 发送消息（新会话）
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "123 乘以 456 是多少"}'

# 多轮对话（传入 session_id）
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "再乘以 2", "session_id": "sess_xxx"}'

# 查看所有会话
curl http://localhost:8080/sessions

# 删除会话
curl -X DELETE http://localhost:8080/sessions/sess_xxx
```

## 扩展新工具

实现 `Tool` 接口即可，框架自动集成：

```go
type MyTool struct{}

func (t *MyTool) Name() string        { return "my_tool" }
func (t *MyTool) Description() string { return "工具描述，LLM 根据这段话决定是否调用" }
func (t *MyTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "input": map[string]interface{}{"type": "string"},
        },
        "required": []string{"input"},
    }
}
func (t *MyTool) Execute(args map[string]interface{}) (string, error) {
    input := args["input"].(string)
    // 你的逻辑
    return "结果", nil
}

// 注册到 Agent
agent.RegisterTool(&MyTool{})
```

## 核心概念

**ReAct 模式**：让 LLM 交替进行推理（Reasoning）和行动（Acting），每次行动后观察结果再继续推理，直到任务完成。相比单次调用，ReAct 能处理需要多步骤、多信息源的复杂任务。

**Function Calling**：OpenAI 标准协议，允许开发者定义工具的 JSON Schema，LLM 根据用户意图自动决定调用哪个工具、传入什么参数。

**并发安全**：多工具并发写入 `results[]` 时，每个 goroutine 写不同的数组下标，天然无竞争，无需加锁。会话 Map 的读写使用 `sync.RWMutex`，读多写少场景下性能优于 `sync.Mutex`。

## 后续可扩展方向

- [ ] 接入 Tavily / Serper 实现真实网络搜索
- [ ] 添加 PostgreSQL 实现持久化会话存储
- [ ] 支持流式输出（SSE）
- [ ] 实现 Agent 任务队列，支持异步长任务
- [ ] 添加 Prometheus 监控指标

## License

MIT
