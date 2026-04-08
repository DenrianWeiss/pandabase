package web

import (
	"embed"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed index.html app.js styles.css
var staticFiles embed.FS

// RegisterFrontend mounts the embedded static files to the router
func RegisterFrontend(router *gin.Engine) {
	// Root route serves index.html
	router.GET("/", func(c *gin.Context) {
		content, err := staticFiles.ReadFile("index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to load frontend")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	})

	// Serve other static assets
	router.GET("/app.js", func(c *gin.Context) {
		content, err := staticFiles.ReadFile("app.js")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", content)
	})

	router.GET("/styles.css", func(c *gin.Context) {
		content, err := staticFiles.ReadFile("styles.css")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/css; charset=utf-8", content)
	})

	// Add a catch-all for SPA navigation if needed (optional since we just have one index file right now)
	router.NoRoute(func(c *gin.Context) {
		// If path starts with /api, return typical 404
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
			return
		}

		// Otherwise serve frontend for SPA routing
		content, err := staticFiles.ReadFile("index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to load frontend")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	})
}
