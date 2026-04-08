package api

import (
	"github.com/gin-gonic/gin"
	
	"pandabase/internal/api/handlers"
	"pandabase/internal/retriever"
)

// SetupRouter configures the Gin router and registers all endpoints
func SetupRouter(r *retriever.Retriever) *gin.Engine {
	router := gin.Default()

	// Initialize handlers
	retrieverHandler := handlers.NewRetrieverHandler(r)

	// API version grouping
	v1 := router.Group("/api/v1")
	{
		// Retriever endpoints
		search := v1.Group("/search")
		{
			// Post request for advanced search configuration
			search.POST("", retrieverHandler.Search)
		}
		
		chunks := v1.Group("/chunks")
		{
			// Get specific chunk for its content
			chunks.GET("/:id", retrieverHandler.GetChunkByID)
		}
		
		documents := v1.Group("/documents")
		{
			// Get all chunks for a specific document
			documents.GET("/:id/chunks", retrieverHandler.GetDocumentChunks)
		}
	}

	return router
}