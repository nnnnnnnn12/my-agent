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
	if len(os.Args) > 1 && os.Args[1] == "server" {
		runServer()
	} else {
		runCLI()
	}
}

func runServer() {
	os.MkdirAll("./data", 0755)
	os.MkdirAll("./output", 0755)

	server, err := api.NewServer("./data/sessions.db")
	if err != nil {
		fmt.Printf("❌ 服务启动失败: %v\n", err)
		os.Exit(1)
	}

	// 托管前端界面
	server.ServeStatic("./frontend")
	fmt.Println("🌐 前端界面: http://localhost:8080")

	if err := server.Run("localhost:8080"); err != nil {
		fmt.Printf("❌ 运行失败: %v\n", err)
		os.Exit(1)
	}
}

func runCLI() {
	fmt.Println("🤖 My Agent — CLI 模式")
	fmt.Println("输入 'quit' 退出，'new' 新建会话")
	fmt.Println("提示: 用 'go run . server' 启动带前端的HTTP服务\n")

	systemPrompt := `你是一个专业的AI助手GoAgent。
你有calculator、web_search和file_writer工具可以使用。
需要实时信息时用web_search，需要计算时用calculator，
用户要保存内容时用file_writer。所有回复使用中文。`

	newAgent := func() *agent.ReActAgent {
		ag := agent.NewReActAgent(systemPrompt)
		ag.RegisterTool(tools.NewCalculatorTool())
		ag.RegisterTool(tools.NewTavilyTool())
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