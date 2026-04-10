package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"

	"pandabase/internal/config"
	"pandabase/internal/db"
	"pandabase/internal/document"
	"pandabase/internal/embedder"
	"pandabase/internal/mcp"
	"pandabase/internal/queue"
	"pandabase/internal/retriever"
	"pandabase/internal/storage"
	"pandabase/pkg/plugin"
)

func main() {
	var mode string
	var port int
	flag.StringVar(&mode, "mode", "stdio", "MCP transport mode (stdio or http)")
	flag.IntVar(&port, "port", 8081, "Port for HTTP mode")
	flag.Parse()

	// Initialize logger
	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	// Determine config path
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			configPath = "config.yaml"
		} else if _, err := os.Stat("../config.yaml"); err == nil {
			configPath = "../config.yaml"
		} else {
			home, _ := os.UserHomeDir()
			if home != "" {
				p := filepath.Join(home, ".pandabase", "config.yaml")
				if _, err := os.Stat(p); err == nil {
					configPath = p
				}
			}
		}
	}

	if configPath == "" {
		fmt.Fprintf(os.Stderr, "Error: Configuration file not found.\n")
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	if cfg.Log.Level != "" {
		level, err := logrus.ParseLevel(cfg.Log.Level)
		if err == nil {
			logger.SetLevel(level)
		}
	}

	database, err := db.New(&cfg.Database, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize database: %v\n", err)
		os.Exit(1)
	}

	if err := database.Initialize(cfg.Embedding.Dimensions); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize database with dimensions: %v\n", err)
		os.Exit(1)
	}

	// Initialize storage
	store, err := storage.NewStorage(&cfg.Storage)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	// Initialize queue client
	redisAddr := fmt.Sprintf("%s:%s", cfg.Redis.Host, cfg.Redis.Port)
	queueClient := queue.NewClient(redisAddr)
	defer queueClient.Close()

	// Initialize document service
	docService := document.NewService(database.DB, store, queueClient, logger)

	var emb plugin.Embedder
	if cfg.Embedding.APIKey != "" {
		factory := embedder.NewFactory(&cfg.Embedding)
		emb, err = factory.Create()
		if err != nil {
			logger.Warnf("Failed to initialize embedder: %v", err)
		}
	}

	if emb == nil {
		fmt.Fprintf(os.Stderr, "Error: Embedder not initialized.\n")
		os.Exit(1)
	}

	ret := retriever.NewRetriever(database.DB, emb, cfg.Database.FTSDictionary, database.GetVectorType())
	server := mcp.NewServer(ret, docService, logger)

	if mode == "http" {
		logger.Infof("Starting MCP server over HTTP on :%d", port)
		mux := http.NewServeMux()
		mux.HandleFunc("/mcp/sse", server.HandleSSEHTTP)
		mux.HandleFunc("/mcp/messages", server.HandlePostHTTP)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
			fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
			os.Exit(1)
		}
	} else {
		logger.Info("Starting MCP server over stdio")
		if err := server.Run(&mcp.StdioTransport{}); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
			os.Exit(1)
		}
	}
}
