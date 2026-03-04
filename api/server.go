package api

import (
	"fmt"
	"my-agent/agent"
	"my-agent/tools"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Server 封装了HTTP服务和Agent实例管理
type Server struct {
	router   *gin.Engine
	sessions map[string]*agent.ReActAgent // sessionID → Agent实例
	mu       sync.RWMutex                 // 保护sessions的并发读写
}

// ChatRequest 前端发来的请求体
type ChatRequest struct {
	Message   string `json:"message" binding:"required"`
	SessionID string `json:"session_id"` // 可选，不传则创建新会话
}

// ChatResponse 返回给前端的响应体
type ChatResponse struct {
	Reply     string `json:"reply"`
	SessionID string `json:"session_id"`
	Steps     int    `json:"steps_hint"` // 提示信息
	Error     string `json:"error,omitempty"`
}

func NewServer() *Server {
	gin.SetMode(gin.ReleaseMode) // 关掉gin的调试日志，生产环境更干净

	s := &Server{
		router:   gin.New(),
		sessions: make(map[string]*agent.ReActAgent),
	}

	s.setupMiddleware()
	s.setupRoutes()
	return s
}

func (s *Server) setupMiddleware() {
	// 日志中间件：记录每个请求
	s.router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("[%s] %s %s %d %s\n",
			param.TimeStamp.Format("15:04:05"),
			param.Method,
			param.Path,
			param.StatusCode,
			param.Latency,
		)
	}))

	// 恢复中间件：panic时不崩溃，返回500
	s.router.Use(gin.Recovery())

	// CORS：允许前端跨域请求（开发阶段很有用）
	s.router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})
}

func (s *Server) setupRoutes() {
	// 健康检查——运维/面试必问
	s.router.GET("/health", s.handleHealth)

	// 核心API
	s.router.POST("/chat", s.handleChat)

	// 查看所有活跃会话
	s.router.GET("/sessions", s.handleListSessions)

	// 删除指定会话（重置对话）
	s.router.DELETE("/sessions/:id", s.handleDeleteSession)
}

// Run 启动服务
func (s *Server) Run(addr string) error {
	fmt.Printf("🚀 Agent HTTP服务启动: http://%s\n", addr)
	fmt.Printf("   POST /chat          发送消息\n")
	fmt.Printf("   GET  /health        健康检查\n")
	fmt.Printf("   GET  /sessions      查看会话列表\n")
	fmt.Printf("   DELETE /sessions/:id 删除会话\n\n")
	return s.router.Run(addr)
}

// ===== Handler 实现 =====

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"time":     time.Now().Format("2006-01-02 15:04:05"),
		"sessions": s.sessionCount(),
	})
}

func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ChatResponse{
			Error: "请求格式错误: " + err.Error(),
		})
		return
	}

	// 获取或创建Agent会话
	sessionID := req.SessionID
	ag := s.getOrCreateAgent(sessionID)
	if sessionID == "" {
		// 新会话，生成ID返回给客户端
		sessionID = generateSessionID()
		s.saveAgent(sessionID, ag)
	}

	// 调用Agent（关闭verbose，API模式下不需要打印到终端）
	ag.Verbose = false
	reply, err := ag.Run(req.Message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ChatResponse{
			Error:     err.Error(),
			SessionID: sessionID,
		})
		return
	}

	c.JSON(http.StatusOK, ChatResponse{
		Reply:     reply,
		SessionID: sessionID,
	})
}

func (s *Server) handleListSessions(c *gin.Context) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}

	c.JSON(http.StatusOK, gin.H{
		"count":    len(ids),
		"sessions": ids,
	})
}

func (s *Server) handleDeleteSession(c *gin.Context) {
	id := c.Param("id")

	s.mu.Lock()
	_, exists := s.sessions[id]
	if exists {
		delete(s.sessions, id)
	}
	s.mu.Unlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "会话已删除"})
}

// ===== 工具方法 =====

func (s *Server) getOrCreateAgent(sessionID string) *agent.ReActAgent {
	if sessionID != "" {
		s.mu.RLock()
		ag, exists := s.sessions[sessionID]
		s.mu.RUnlock()
		if exists {
			return ag
		}
	}
	return s.newAgent()
}

func (s *Server) saveAgent(sessionID string, ag *agent.ReActAgent) {
	s.mu.Lock()
	s.sessions[sessionID] = ag
	s.mu.Unlock()
}

func (s *Server) sessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func (s *Server) newAgent() *agent.ReActAgent {
	systemPrompt := `你是一个专业的AI助手GoAgent，通过HTTP API提供服务。
你有calculator和file_writer工具可以使用。
回复简洁专业，使用中文。`

	ag := agent.NewReActAgent(systemPrompt)
	ag.RegisterTool(tools.NewCalculatorTool())
	ag.RegisterTool(tools.NewFileTool("./output"))
	return ag
}

func generateSessionID() string {
	return fmt.Sprintf("sess_%d", time.Now().UnixNano())
}