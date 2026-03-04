package main

import (
	"bufio"
	"fmt"
	"my-agent/agent"
	"my-agent/api"
	"my-agent/tools"
	"os"
	"strings"
)

func main() {
	// 根据启动参数决定运行模式
	// go run . server  → 启动HTTP服务
	// go run .         → 命令行对话模式
	if len(os.Args) > 1 && os.Args[1] == "server" {
		runServer()
	} else {
		runCLI()
	}
}

func runServer() {
	server := api.NewServer()
	if err := server.Run("localhost:8080"); err != nil {
		fmt.Printf("服务启动失败: %v\n", err)
		os.Exit(1)
	}
}

func runCLI() {
	fmt.Println("🤖 My Agent - 第四周：并发 + HTTP API")
	fmt.Println("========================================")
	fmt.Println("命令行模式 | 输入 'quit' 退出，'new' 新建会话")
	fmt.Println("提示：用 'go run . server' 启动HTTP服务模式\n")

	systemPrompt := `你是一个专业的AI助手GoAgent。
你有calculator和file_writer工具可以使用。
遇到计算一定用工具，不要自己估算。所有回复用中文。`

	newAgent := func() *agent.ReActAgent {
		ag := agent.NewReActAgent(systemPrompt)
		ag.RegisterTool(tools.NewCalculatorTool())
		ag.RegisterTool(tools.NewFileTool("./output"))
		return ag
	}

	myAgent := newAgent()
	fmt.Println("✅ Agent就绪\n")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for {
		fmt.Print("你: ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "quit" {
			fmt.Println("👋 再见！")
			break
		}
		if input == "new" {
			myAgent = newAgent()
			fmt.Println("🔄 新会话已开始\n")
			continue
		}

		reply, err := myAgent.Run(input)
		if err != nil {
			fmt.Printf("❌ 错误: %v\n\n", err)
			continue
		}
		fmt.Printf("\n🤖 GoAgent: %s\n\n", reply)
	}
}