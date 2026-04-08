package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"pandabase/internal/queue"
)

// QueueHandler handles queue status and management HTTP requests
type QueueHandler struct {
	inspector *queue.Inspector
}

// NewQueueHandler creates a new queue handler
func NewQueueHandler(inspector *queue.Inspector) *QueueHandler {
	return &QueueHandler{inspector: inspector}
}

// GetStats returns queue statistics
func (h *QueueHandler) GetStats(c *gin.Context) {
	stats, err := h.inspector.GetQueueStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"queues": stats,
	})
}

// ListTasksRequest represents task list request parameters
type ListTasksRequest struct {
	Queue    string `form:"queue" binding:"required"`
	State    string `form:"state" binding:"required"` // pending, active, scheduled, retry, archived, completed
	Page     int    `form:"page,default=1"`
	PageSize int    `form:"page_size,default=20"`
}

// ListTasks lists tasks in a queue by state
func (h *QueueHandler) ListTasks(c *gin.Context) {
	var req ListTasksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 || req.PageSize > 100 {
		req.PageSize = 20
	}

	var tasks []queue.TaskInfo
	var err error

	switch req.State {
	case "pending":
		tasks, err = h.inspector.ListPendingTasks(req.Queue, req.Page, req.PageSize)
	case "active":
		tasks, err = h.inspector.ListActiveTasks(req.Queue, req.Page, req.PageSize)
	case "scheduled":
		tasks, err = h.inspector.ListScheduledTasks(req.Queue, req.Page, req.PageSize)
	case "retry":
		tasks, err = h.inspector.ListRetryTasks(req.Queue, req.Page, req.PageSize)
	case "archived":
		tasks, err = h.inspector.ListArchivedTasks(req.Queue, req.Page, req.PageSize)
	case "completed":
		tasks, err = h.inspector.ListCompletedTasks(req.Queue, req.Page, req.PageSize)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      tasks,
		"page":      req.Page,
		"page_size": req.PageSize,
	})
}

// DeleteTaskRequest represents task deletion request
type DeleteTaskRequest struct {
	Queue string `json:"queue" binding:"required"`
	TaskID string `json:"task_id" binding:"required"`
}

// DeleteTask deletes a task from a queue
func (h *QueueHandler) DeleteTask(c *gin.Context) {
	var req DeleteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.inspector.DeleteTask(req.Queue, req.TaskID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "task deleted"})
}

// DeleteAllArchivedTasks deletes all archived tasks from a queue
func (h *QueueHandler) DeleteAllArchivedTasks(c *gin.Context) {
	queue := c.Param("queue")
	if queue == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "queue is required"})
		return
	}

	_, err := h.inspector.DeleteAllArchivedTasks(queue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "all archived tasks deleted"})
}

// ArchiveAllRetryTasks archives all retry tasks in a queue
func (h *QueueHandler) ArchiveAllRetryTasks(c *gin.Context) {
	queue := c.Param("queue")
	if queue == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "queue is required"})
		return
	}

	_, err := h.inspector.ArchiveAllRetryTasks(queue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "all retry tasks archived"})
}

// RunAllScheduledTasks runs all scheduled tasks immediately
func (h *QueueHandler) RunAllScheduledTasks(c *gin.Context) {
	queue := c.Param("queue")
	if queue == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "queue is required"})
		return
	}

	_, err := h.inspector.RunAllScheduledTasks(queue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "all scheduled tasks queued for immediate execution"})
}

// PauseQueue pauses a queue
func (h *QueueHandler) PauseQueue(c *gin.Context) {
	queue := c.Param("queue")
	if queue == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "queue is required"})
		return
	}

	if err := h.inspector.PauseQueue(queue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "queue paused"})
}

// UnpauseQueue unpauses a queue
func (h *QueueHandler) UnpauseQueue(c *gin.Context) {
	queue := c.Param("queue")
	if queue == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "queue is required"})
		return
	}

	if err := h.inspector.UnpauseQueue(queue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "queue unpaused"})
}
