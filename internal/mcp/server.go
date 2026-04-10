package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"pandabase/internal/document"
	"pandabase/internal/retriever"
)

// JSONRPCMessage represents a basic JSON-RPC 2.0 message
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Transport defines the interface for MCP message delivery
type Transport interface {
	Send(msg JSONRPCMessage) error
	Receive() (<-chan JSONRPCMessage, <-chan error)
}

// StdioTransport implements Transport using os.Stdin and os.Stdout
type StdioTransport struct {
	logger *logrus.Logger
}

func (t *StdioTransport) Send(msg JSONRPCMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func (t *StdioTransport) Receive() (<-chan JSONRPCMessage, <-chan error) {
	out := make(chan JSONRPCMessage)
	errs := make(chan error)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					errs <- err
				}
				close(out)
				close(errs)
				return
			}
			var msg JSONRPCMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				errs <- err
				continue
			}
			out <- msg
		}
	}()
	return out, errs
}

// Server handles MCP protocol
type Server struct {
	retriever *retriever.Retriever
	docService *document.Service
	logger    *logrus.Logger
	sessions  map[string]*httpTransport
	mu        sync.RWMutex
}

// NewServer creates a new MCP server
func NewServer(r *retriever.Retriever, ds *document.Service, logger *logrus.Logger) *Server {
	if logger == nil {
		logger = logrus.New()
	}
	return &Server{
		retriever:  r,
		docService: ds,
		logger:     logger,
		sessions:   make(map[string]*httpTransport),
	}
}

// Run starts the MCP server using a specific transport
func (s *Server) Run(t Transport) error {
	messages, errors := t.Receive()
	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			if msg.Method != "" {
				s.handleRequest(t, &msg)
			}
		case err, ok := <-errors:
			if !ok {
				return nil
			}
			s.logger.Errorf("Transport error: %v", err)
		}
	}
}

func (s *Server) handleRequest(t Transport, msg *JSONRPCMessage) {
	switch msg.Method {
	case "initialize":
		s.sendResult(t, msg.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"serverInfo": map[string]string{
				"name":    "pandabase",
				"version": "0.1.0",
			},
		})

	case "notifications/initialized":
		// Ignore

	case "tools/list":
		s.sendResult(t, msg.ID, map[string]any{
			"tools": []map[string]any{
				{
					"name":        "search",
					"description": "Search for documents in the knowledge base using semantic and full-text search.",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "The search query string",
							},
							"top_k": map[string]any{
								"type":        "integer",
								"description": "Maximum number of results to return (default: 5, max: 20)",
								"default":     5,
							},
							"namespace_ids": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "string",
								},
								"description": "Optional list of namespace IDs to filter by",
							},
						},
						"required": []string{"query"},
					},
				},
				{
					"name":        "ingest",
					"description": "Ingest direct text content into a namespace.",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"namespace_id": map[string]any{
								"type":        "string",
								"description": "The namespace ID to ingest into",
							},
							"filename": map[string]any{
								"type":        "string",
								"description": "The name for the document",
							},
							"content": map[string]any{
								"type":        "string",
								"description": "The text content to ingest",
							},
						},
						"required": []string{"namespace_id", "filename", "content"},
					},
				},
				{
					"name":        "import_url",
					"description": "Request the server to fetch and index content from a URL.",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"namespace_id": map[string]any{
								"type":        "string",
								"description": "The namespace ID to import into",
							},
							"url": map[string]any{
								"type":        "string",
								"description": "The URL to fetch",
							},
							"parser_type": map[string]any{
								"type":        "string",
								"description": "Parser type (web, notion)",
								"enum":        []string{"web", "notion"},
								"default":     "web",
							},
						},
						"required": []string{"namespace_id", "url"},
					},
				},
			},
		})

	case "tools/call":
		s.handleToolCall(t, msg)

	default:
		s.sendError(t, msg.ID, -32601, "Method not found", fmt.Sprintf("Method '%s' not found", msg.Method))
	}
}

func (s *Server) handleToolCall(t Transport, msg *JSONRPCMessage) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.sendError(t, msg.ID, -32602, "Invalid params", err.Error())
		return
	}

	switch params.Name {
	case "search":
		s.handleSearch(t, msg, params.Arguments)
	case "ingest":
		s.handleIngest(t, msg, params.Arguments)
	case "import_url":
		s.handleImportURL(t, msg, params.Arguments)
	default:
		s.sendError(t, msg.ID, -32601, "Tool not found", fmt.Sprintf("Tool '%s' not found", params.Name))
	}
}

func (s *Server) handleSearch(t Transport, msg *JSONRPCMessage, arguments json.RawMessage) {
	var args struct {
		Query        string   `json:"query"`
		TopK         int      `json:"top_k"`
		NamespaceIDs []string `json:"namespace_ids"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		s.sendError(t, msg.ID, -32602, "Invalid arguments", err.Error())
		return
	}

	if args.TopK <= 0 {
		args.TopK = 5
	}
	if args.TopK > 20 {
		args.TopK = 20
	}

	req := retriever.SearchRequest{
		Query:        args.Query,
		TopK:         args.TopK,
		NamespaceIDs: args.NamespaceIDs,
		Mode:         retriever.SearchModeHybrid,
	}

	resp, err := s.retriever.Search(context.Background(), req)
	if err != nil {
		s.sendToolError(t, msg.ID, fmt.Sprintf("Search failed: %v", err))
		return
	}

	var resultText string
	if len(resp.Results) == 0 {
		resultText = "No relevant results found."
	} else {
		for i, res := range resp.Results {
			resultText += fmt.Sprintf("[%d] Score: %.4f\nDocument: %s (%s)\nContent: %s\n\n",
				i+1, res.FinalScore, res.Document.SourceURI, res.Document.SourceType, res.Chunk.Content)
		}
	}

	s.sendToolResult(t, msg.ID, resultText)
}

func (s *Server) handleIngest(t Transport, msg *JSONRPCMessage, arguments json.RawMessage) {
	var args struct {
		NamespaceID string `json:"namespace_id"`
		Filename    string `json:"filename"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		s.sendError(t, msg.ID, -32602, "Invalid arguments", err.Error())
		return
	}

	nsID, err := uuid.Parse(args.NamespaceID)
	if err != nil {
		s.sendToolError(t, msg.ID, fmt.Sprintf("Invalid namespace ID: %v", err))
		return
	}

	// We don't have a user ID here in stdio mode easily, but for MCP usually it's the owner/admin
	// In HTTP mode we have the user ID from context.
	// For now, let's assume we need to skip permission check or use a system user ID if we're in stdio.
	// Actually, let's try to get it from context if it's SSE, otherwise use Nil for stdio (which service might reject).
	
	userID := uuid.Nil // Default for stdio/unauthenticated
	
	result, err := s.docService.IngestText(context.Background(), nsID, userID, args.Filename, args.Content, document.UploadOptions{})
	if err != nil {
		s.sendToolError(t, msg.ID, fmt.Sprintf("Ingest failed: %v", err))
		return
	}

	s.sendToolResult(t, msg.ID, fmt.Sprintf("Successfully ingested document %s. Status: %s. Message: %s", 
		result.DocumentID, result.Status, result.Message))
}

func (s *Server) handleImportURL(t Transport, msg *JSONRPCMessage, arguments json.RawMessage) {
	var args struct {
		NamespaceID string `json:"namespace_id"`
		URL         string `json:"url"`
		ParserType  string `json:"parser_type"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		s.sendError(t, msg.ID, -32602, "Invalid arguments", err.Error())
		return
	}

	nsID, err := uuid.Parse(args.NamespaceID)
	if err != nil {
		s.sendToolError(t, msg.ID, fmt.Sprintf("Invalid namespace ID: %v", err))
		return
	}

	if args.ParserType == "" {
		args.ParserType = "web"
	}

	userID := uuid.Nil
	result, err := s.docService.ImportURL(context.Background(), document.ImportRequest{
		NamespaceID: nsID,
		UserID:      userID,
		URL:         args.URL,
		SourceType:  args.ParserType,
		Options: document.UploadOptions{
			RenderFallback: true,
		},
	})
	if err != nil {
		s.sendToolError(t, msg.ID, fmt.Sprintf("Import URL failed: %v", err))
		return
	}

	s.sendToolResult(t, msg.ID, fmt.Sprintf("Successfully queued import for URL %s. Document ID: %s. Task ID: %s", 
		args.URL, result.DocumentID, result.TaskID))
}

func (s *Server) sendToolResult(t Transport, id any, text string) {
	s.sendResult(t, id, map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
	})
}

func (s *Server) sendToolError(t Transport, id any, text string) {
	s.sendResult(t, id, map[string]any{
		"isError": true,
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
	})
}

func (s *Server) sendResult(t Transport, id any, result any) {
	t.Send(JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) sendError(t Transport, id any, code int, message string, data any) {
	t.Send(JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

// Gin handlers for MCP HTTP Transport

// HandleSSE manages the SSE connection for MCP
func (s *Server) HandleSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	sessionID := c.Query("session")
	if sessionID == "" {
		sessionID = "default"
	}

	transport := &httpTransport{
		messages: make(chan JSONRPCMessage, 10),
		errors:   make(chan error, 1),
		w:        c.Writer,
		flusher:  c.Writer.(http.Flusher),
	}

	s.mu.Lock()
	s.sessions[sessionID] = transport
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
	}()

	s.Run(transport)
}

// HandlePost receives messages for a specific MCP session
func (s *Server) HandlePost(c *gin.Context) {
	sessionID := c.Query("session")
	if sessionID == "" {
		sessionID = "default"
	}

	s.mu.RLock()
	transport, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	var msg JSONRPCMessage
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON-RPC message"})
		return
	}

	transport.messages <- msg
	c.Status(http.StatusAccepted)
}

// HandleSSEHTTP manages the SSE connection for MCP (standard http.HandlerFunc)
func (s *Server) HandleSSEHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		sessionID = "default"
	}

	transport := &httpTransport{
		messages: make(chan JSONRPCMessage, 10),
		errors:   make(chan error, 1),
		w:        w,
		flusher:  flusher,
	}

	s.mu.Lock()
	s.sessions[sessionID] = transport
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
	}()

	s.Run(transport)
}

// HandlePostHTTP receives messages for a specific MCP session (standard http.HandlerFunc)
func (s *Server) HandlePostHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		sessionID = "default"
	}

	s.mu.RLock()
	transport, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	var msg JSONRPCMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Invalid message", http.StatusBadRequest)
		return
	}

	transport.messages <- msg
	w.WriteHeader(http.StatusAccepted)
}

type httpTransport struct {
	messages chan JSONRPCMessage
	errors   chan error
	w        http.ResponseWriter
	flusher  http.Flusher
}

func (t *httpTransport) Send(msg JSONRPCMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Fprintf(t.w, "data: %s\n\n", string(data))
	t.flusher.Flush()
	return nil
}

func (t *httpTransport) Receive() (<-chan JSONRPCMessage, <-chan error) {
	return t.messages, t.errors
}
