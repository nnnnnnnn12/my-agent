package main

import (
	"bufio"
	"fmt"
	"my-agent/agent"
	"my-agent/api"
	"my-agent/rag"
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

	server, err := api.NewServer("./data/sessions.db", "./data/vectors.json")
	if err != nil {
		fmt.Printf("❌ 启动失败: %v\n", err)
		os.Exit(1)
	}
	server.ServeStatic("./frontend")
	fmt.Println("🌐 前端界面: http://localhost:8080")
	if err := server.Run("localhost:8080"); err != nil {
		fmt.Printf("❌ 运行失败: %v\n", err)
		os.Exit(1)
	}
}

func runCLI() {
	fmt.Println("🤖 GoAgent — CLI模式（含RAG）")
	fmt.Println("输入 'quit' 退出，'new' 新建会话，'upload <文件路径>' 上传文档\n")

	ragEngine := rag.NewEngine("./data/vectors.json")
	os.MkdirAll("./data", 0755)

	systemPrompt := `你是GoAgent。工具：calculator、web_search、knowledge_search、file_writer。
优先用knowledge_search回答已上传文档的问题。所有回复使用中文。`

	newAgent := func() *agent.ReActAgent {
		ag := agent.NewReActAgent(systemPrompt)
		ag.RegisterTool(tools.NewCalculatorTool())
		ag.RegisterTool(tools.NewTavilyTool())
		ag.RegisterTool(tools.NewRAGSearchTool(ragEngine))
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
			fmt.Println("🔄 新会话\n")
			continue
		}
		// 上传文档命令：upload ./path/to/file.md
		if strings.HasPrefix(input, "upload ") {
			path := strings.TrimPrefix(input, "upload ")
			path = strings.TrimSpace(path)
			content, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("❌ 读取文件失败: %v\n\n", err)
				continue
			}
			name := path[strings.LastIndexAny(path, "/\\")+1:]
			docID := fmt.Sprintf("doc_%d", len(path))
			if err := ragEngine.AddDocument(docID, name, string(content)); err != nil {
				fmt.Printf("❌ 索引失败: %v\n\n", err)
			} else {
				myAgent = newAgent() // 重建Agent以感知新文档
				fmt.Printf("✅ 文档已加入知识库\n\n")
			}
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