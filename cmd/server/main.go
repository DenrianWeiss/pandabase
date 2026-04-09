package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"pandabase/internal/api"
	"pandabase/internal/api/handlers"
	"pandabase/internal/auth"
	"pandabase/internal/config"
	"pandabase/internal/db"
	"pandabase/internal/document"
	"pandabase/internal/embedder"
	"pandabase/internal/namespace"
	"pandabase/internal/queue"
	"pandabase/internal/retriever"
	"pandabase/internal/setup"
	"pandabase/internal/storage"
	"pandabase/pkg/plugin"
	"pandabase/web"
)

func main() {
	// Initialize logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	// Determine config path
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	// Check if config exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Info("Configuration file not found. Starting setup wizard on :8080...")
		startSetupMode(configPath, logger)
		return
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Set log level from config
	if cfg.Log.Level != "" {
		level, err := logrus.ParseLevel(cfg.Log.Level)
		if err == nil {
			logger.SetLevel(level)
		}
	}

	// Initialize database
	database, err := db.New(&cfg.Database, logger)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize database with embedding dimensions
	if err := database.Initialize(cfg.Embedding.Dimensions); err != nil {
		logger.Fatalf("Failed to initialize database with dimensions: %v", err)
	}

	// Initialize storage
	store, err := storage.NewStorage(&cfg.Storage)
	if err != nil {
		logger.Fatalf("Failed to initialize storage: %v", err)
	}

	// Initialize embedder
	var emb plugin.Embedder
	if cfg.Embedding.APIKey != "" {
		factory := embedder.NewFactory(&cfg.Embedding)
		emb, err = factory.Create()
		if err != nil {
			logger.Warnf("Failed to initialize embedder: %v", err)
		}
	}

	// Build Redis address
	redisAddr := fmt.Sprintf("%s:%s", cfg.Redis.Host, cfg.Redis.Port)

	// Initialize queue client
	queueClient := queue.NewClient(redisAddr)
	defer queueClient.Close()

	// Initialize queue inspector
	queueInspector := queue.NewInspector(redisAddr)
	defer queueInspector.Close()

	// Initialize auth service
	authConfig := buildAuthConfig(&cfg.Auth)
	authService := auth.NewService(database.DB, authConfig)
	var oauthService *auth.OAuthService
	if authConfig.EnableOAuth {
		oauthService = auth.NewOAuthService(database.DB, authService, authConfig)
	}

	// Initialize document and namespace service
	docService := document.NewService(database.DB, store, queueClient, logger)
	nsService := namespace.NewService(database.DB)

	// Initialize retriever
	ret := retriever.NewRetriever(database.DB, emb, cfg.Database.FTSDictionary, database.GetVectorType())

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(authService, oauthService)
	docHandler := handlers.NewDocumentHandler(docService)
	queueHandler := handlers.NewQueueHandler(queueInspector)
	retrieverHandler := handlers.NewRetrieverHandler(ret)
	nsHandler := handlers.NewNamespaceHandler(nsService)
	userHandler := handlers.NewUserHandler(authService)

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(loggingMiddleware(logger))

	// Setup routes
	api.SetupRoutes(router, authHandler, userHandler, docHandler, queueHandler, retrieverHandler, nsHandler, authService, oauthService)

	// Setup embed frontend
	web.RegisterFrontend(router)

	// Create HTTP server
	serverAddr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: router,
	}

	// Start worker in background
	workerCtx, workerCancel := context.WithCancel(context.Background())
	go runWorker(workerCtx, redisAddr, database.DB, store, emb, logger)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Infof("Server starting on %s", serverAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Failed to start server: %v", err)
		}
	}()

	<-quit
	logger.Info("Shutting down server...")

	// Stop worker
	workerCancel()

	// Shutdown server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("Server forced to shutdown: %v", err)
	}

	logger.Info("Server exited")
}

// loggingMiddleware creates a Gin middleware for request logging
func loggingMiddleware(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()

		if raw != "" {
			path = path + "?" + raw
		}

		logger.WithFields(logrus.Fields{
			"status":    statusCode,
			"latency":   latency,
			"client_ip": clientIP,
			"method":    method,
			"path":      path,
		}).Info("Request")
	}
}

// runWorker runs the background task worker
func runWorker(ctx context.Context, redisAddr string, db *gorm.DB, store storage.Storage, emb plugin.Embedder, logger *logrus.Logger) {
	processor := queue.NewProcessor(db, store, emb, logger)

	mux := asynq.NewServeMux()
	processor.RegisterHandlers(mux)

	server := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: 10,
			Queues: map[string]int{
				queue.QueueCritical: 6,
				queue.QueueDefault:  3,
				queue.QueueLow:      1,
			},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				logger.WithError(err).WithField("task_type", task.Type()).Error("Task failed")
			}),
		},
	)

	logger.Info("Starting background worker...")
	if err := server.Run(mux); err != nil {
		logger.Errorf("Worker error: %v", err)
	}

	<-ctx.Done()
	logger.Info("Stopping background worker...")
	server.Shutdown()
}

// buildAuthConfig converts config.AuthConfig to auth.Config
func buildAuthConfig(cfg *config.AuthConfig) auth.Config {
	jwtExpiry, _ := time.ParseDuration(cfg.JWTExpiry)
	if jwtExpiry == 0 {
		jwtExpiry = 24 * time.Hour
	}
	refreshExpiry, _ := time.ParseDuration(cfg.RefreshTokenExpiry)
	if refreshExpiry == 0 {
		refreshExpiry = 7 * 24 * time.Hour
	}

	jwtSecret := cfg.JWTSecret
	if jwtSecret == "" {
		jwtSecret = auth.GenerateRandomSecret()
	}

	return auth.Config{
		JWTSecret:          jwtSecret,
		JWTExpiry:          jwtExpiry,
		RefreshTokenExpiry: refreshExpiry,
		EnableOAuth:        cfg.EnableOAuth,
		OAuthProviders: auth.OAuthConfig{
			Google: auth.OAuthProviderConfig{
				Enabled:      cfg.OAuthProviders.Google.Enabled,
				ClientID:     cfg.OAuthProviders.Google.ClientID,
				ClientSecret: cfg.OAuthProviders.Google.ClientSecret,
				RedirectURL:  cfg.OAuthProviders.Google.RedirectURL,
			},
			GitHub: auth.OAuthProviderConfig{
				Enabled:      cfg.OAuthProviders.GitHub.Enabled,
				ClientID:     cfg.OAuthProviders.GitHub.ClientID,
				ClientSecret: cfg.OAuthProviders.GitHub.ClientSecret,
				RedirectURL:  cfg.OAuthProviders.GitHub.RedirectURL,
			},
		},
	}
}

func startSetupMode(configPath string, logger *logrus.Logger) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	// Simple logging for setup mode
	router.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Infof("%s %s %d %v", c.Request.Method, c.Request.URL.Path, c.Writer.Status(), time.Since(start))
	})

	handler := setup.NewSetupHandler(configPath)
	handler.RegisterRoutes(router)

	// Add a redirect from root to setup
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/setup")
	})

	server := &http.Server{
		Addr:    "0.0.0.0:8080",
		Handler: router,
	}

	logger.Info("Setup wizard is available at http://localhost:8080/setup")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("Setup server failed: %v", err)
	}
}
