package api

import (
	"github.com/gin-gonic/gin"

	"pandabase/internal/api/handlers"
	"pandabase/internal/auth"
)

// SetupRoutes configures all API routes
func SetupRoutes(
	router *gin.Engine,
	authHandler *handlers.AuthHandler,
	docHandler *handlers.DocumentHandler,
	queueHandler *handlers.QueueHandler,
	retrieverHandler *handlers.RetrieverHandler,
	nsHandler *handlers.NamespaceHandler,
	authService *auth.Service,
	oauthService *auth.OAuthService,
) {
	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// API v1 group
	v1 := router.Group("/api/v1")
	{
		// Public routes
		public := v1.Group("")
		{
			// Auth routes
			public.POST("/auth/register", authHandler.Register)
			public.POST("/auth/login", authHandler.Login)
			public.POST("/auth/refresh", authHandler.RefreshToken)
			public.GET("/auth/providers", authHandler.GetOAuthProviders)

			// OAuth routes (if enabled)
			if oauthService != nil {
				oauthService.RegisterRoutes(public)
			}
		}

		// Protected routes
		protected := v1.Group("")
		protected.Use(authService.Middleware())
		{
			// User routes
			protected.GET("/auth/me", authHandler.GetMe)

			// Document routes
			docs := protected.Group("/namespaces/:namespace_id/documents")
			{
				docs.GET("", docHandler.List)
				docs.POST("", docHandler.Upload)
				docs.GET("/:document_id", docHandler.Get)
				docs.PUT("/:document_id", docHandler.Update)
				docs.DELETE("/:document_id", docHandler.Delete)
				docs.GET("/:document_id/download", docHandler.Download)
			}

			// Queue management routes (admin only)
			queue := protected.Group("/queue")
			{
				queue.GET("/stats", queueHandler.GetStats)
				queue.GET("/tasks", queueHandler.ListTasks)
				queue.POST("/tasks/delete", queueHandler.DeleteTask)
				queue.DELETE("/:queue/archived", queueHandler.DeleteAllArchivedTasks)
				queue.POST("/:queue/retry/archive", queueHandler.ArchiveAllRetryTasks)
				queue.POST("/:queue/scheduled/run", queueHandler.RunAllScheduledTasks)
				queue.POST("/:queue/pause", queueHandler.PauseQueue)
				queue.POST("/:queue/unpause", queueHandler.UnpauseQueue)
			}

			// Namespace routes
			namespaces := protected.Group("/namespaces")
			{
				namespaces.GET("", nsHandler.List)
				namespaces.POST("", nsHandler.Create)
				namespaces.GET("/:id", nsHandler.Get)
				namespaces.PUT("/:id", nsHandler.Update)
				namespaces.DELETE("/:id", nsHandler.Delete)
			}
			
			// Search routes
			protected.POST("/search", retrieverHandler.Search)
			protected.GET("/chunks/:id", retrieverHandler.GetChunkByID)
			protected.GET("/documents/:id/chunks", retrieverHandler.GetDocumentChunks)
		}
	}
}
