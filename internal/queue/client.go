package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const (
	// Task types
	TypeDocumentProcess = "document:process"
	TypeDocumentDelete  = "document:delete"
	TypeDocumentUpdate  = "document:update"

	// Queue priorities
	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueLow      = "low"
)

// TaskPayload represents base task payload
type TaskPayload struct {
	DocumentID  uuid.UUID `json:"document_id"`
	NamespaceID uuid.UUID `json:"namespace_id"`
	UserID      uuid.UUID `json:"user_id"`
	Timestamp   int64     `json:"timestamp"`
}

// DocumentProcessPayload represents document processing task
type DocumentProcessPayload struct {
	TaskPayload
	FilePath       string            `json:"file_path,omitempty"`
	FileName       string            `json:"file_name,omitempty"`
	ContentType    string            `json:"content_type,omitempty"`
	SourceURL      string            `json:"source_url,omitempty"`
	SourceMetadata map[string]any    `json:"source_metadata,omitempty"`
	Options        ProcessingOptions `json:"options"`
}

// ProcessingOptions represents document processing options
type ProcessingOptions struct {
	ChunkSize        int    `json:"chunk_size"`
	ChunkOverlap     int    `json:"chunk_overlap"`
	ParserType       string `json:"parser_type"` // auto, text, markdown
	SkipEmbedding    bool   `json:"skip_embedding"`
	ForceReprocess   bool   `json:"force_reprocess"` // Force reprocess even if hash matches
	RenderJavaScript bool   `json:"render_javascript"`
	RenderTimeout    int    `json:"render_timeout"`
	WaitSelector     string `json:"wait_selector,omitempty"`
	RenderFallback   bool   `json:"render_fallback"`
}

// DocumentDeletePayload represents document deletion task
type DocumentDeletePayload struct {
	TaskPayload
	CascadeDelete bool `json:"cascade_delete"` // Delete chunks and embeddings
}

// Client wraps asynq client
type Client struct {
	client *asynq.Client
}

// NewClient creates a new queue client
func NewClient(redisAddr string) *Client {
	return &Client{
		client: asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr}),
	}
}

// Close closes the client connection
func (c *Client) Close() error {
	return c.client.Close()
}

// EnqueueDocumentProcess enqueues a document processing task
func (c *Client) EnqueueDocumentProcess(ctx context.Context, payload DocumentProcessPayload, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	payload.Timestamp = time.Now().Unix()

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(TypeDocumentProcess, data, opts...)
	return c.client.EnqueueContext(ctx, task)
}

// EnqueueDocumentDelete enqueues a document deletion task
func (c *Client) EnqueueDocumentDelete(ctx context.Context, payload DocumentDeletePayload, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	payload.Timestamp = time.Now().Unix()

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(TypeDocumentDelete, data, opts...)
	return c.client.EnqueueContext(ctx, task)
}

// EnqueueDocumentUpdate enqueues a document update task
func (c *Client) EnqueueDocumentUpdate(ctx context.Context, payload DocumentProcessPayload, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	payload.Timestamp = time.Now().Unix()

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(TypeDocumentUpdate, data, opts...)
	return c.client.EnqueueContext(ctx, task)
}

// DefaultTaskOptions returns default task options
func DefaultTaskOptions() []asynq.Option {
	return []asynq.Option{
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(3),
		asynq.Timeout(30 * time.Minute),
		asynq.Retention(24 * time.Hour),
	}
}

// CriticalTaskOptions returns critical task options
func CriticalTaskOptions() []asynq.Option {
	return []asynq.Option{
		asynq.Queue(QueueCritical),
		asynq.MaxRetry(5),
		asynq.Timeout(60 * time.Minute),
		asynq.Retention(48 * time.Hour),
	}
}

// LowPriorityTaskOptions returns low priority task options
func LowPriorityTaskOptions() []asynq.Option {
	return []asynq.Option{
		asynq.Queue(QueueLow),
		asynq.MaxRetry(2),
		asynq.Timeout(15 * time.Minute),
		asynq.Retention(12 * time.Hour),
	}
}

// ScheduledTaskOptions returns options for scheduled tasks
func ScheduledTaskOptions(processAt time.Time) []asynq.Option {
	return []asynq.Option{
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(3),
		asynq.ProcessAt(processAt),
	}
}

// Inspector provides queue inspection capabilities
type Inspector struct {
	inspector *asynq.Inspector
}

// NewInspector creates a new queue inspector
func NewInspector(redisAddr string) *Inspector {
	return &Inspector{
		inspector: asynq.NewInspector(asynq.RedisClientOpt{Addr: redisAddr}),
	}
}

// Close closes the inspector connection
func (i *Inspector) Close() error {
	return i.inspector.Close()
}

// QueueStats represents statistics for a queue
type QueueStats struct {
	Name      string `json:"name"`
	Pending   int    `json:"pending"`
	Active    int    `json:"active"`
	Scheduled int    `json:"scheduled"`
	Retry     int    `json:"retry"`
	Archived  int    `json:"archived"`
	Completed int    `json:"completed"`
}

// GetQueueStats returns statistics for all queues
func (i *Inspector) GetQueueStats() ([]QueueStats, error) {
	queues := []string{QueueCritical, QueueDefault, QueueLow}
	stats := make([]QueueStats, 0, len(queues))

	for _, queue := range queues {
		info, err := i.inspector.GetQueueInfo(queue)
		if err != nil {
			continue // Queue might not exist yet
		}

		stats = append(stats, QueueStats{
			Name:      queue,
			Pending:   info.Pending,
			Active:    info.Active,
			Scheduled: info.Scheduled,
			Retry:     info.Retry,
			Archived:  info.Archived,
			Completed: info.Completed,
		})
	}

	return stats, nil
}

// TaskInfo represents task information
type TaskInfo struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Queue     string    `json:"queue"`
	State     string    `json:"state"`
	MaxRetry  int       `json:"max_retry"`
	Retried   int       `json:"retried"`
	CreatedAt time.Time `json:"created_at"`
}

// ListPendingTasks lists pending tasks in a queue
func (i *Inspector) ListPendingTasks(queue string, page, pageSize int) ([]TaskInfo, error) {
	tasks, err := i.inspector.ListPendingTasks(queue, asynq.Page(page), asynq.PageSize(pageSize))
	if err != nil {
		return nil, err
	}

	return convertTasks(tasks), nil
}

// ListActiveTasks lists active tasks in a queue
func (i *Inspector) ListActiveTasks(queue string, page, pageSize int) ([]TaskInfo, error) {
	tasks, err := i.inspector.ListActiveTasks(queue, asynq.Page(page), asynq.PageSize(pageSize))
	if err != nil {
		return nil, err
	}

	return convertTasks(tasks), nil
}

// ListScheduledTasks lists scheduled tasks in a queue
func (i *Inspector) ListScheduledTasks(queue string, page, pageSize int) ([]TaskInfo, error) {
	tasks, err := i.inspector.ListScheduledTasks(queue, asynq.Page(page), asynq.PageSize(pageSize))
	if err != nil {
		return nil, err
	}

	return convertTasks(tasks), nil
}

// ListRetryTasks lists retry tasks in a queue
func (i *Inspector) ListRetryTasks(queue string, page, pageSize int) ([]TaskInfo, error) {
	tasks, err := i.inspector.ListRetryTasks(queue, asynq.Page(page), asynq.PageSize(pageSize))
	if err != nil {
		return nil, err
	}

	return convertTasks(tasks), nil
}

// ListArchivedTasks lists archived (failed) tasks in a queue
func (i *Inspector) ListArchivedTasks(queue string, page, pageSize int) ([]TaskInfo, error) {
	tasks, err := i.inspector.ListArchivedTasks(queue, asynq.Page(page), asynq.PageSize(pageSize))
	if err != nil {
		return nil, err
	}

	return convertTasks(tasks), nil
}

// ListCompletedTasks lists completed tasks in a queue
func (i *Inspector) ListCompletedTasks(queue string, page, pageSize int) ([]TaskInfo, error) {
	tasks, err := i.inspector.ListCompletedTasks(queue, asynq.Page(page), asynq.PageSize(pageSize))
	if err != nil {
		return nil, err
	}

	return convertTasks(tasks), nil
}

// DeleteTask deletes a task by ID
func (i *Inspector) DeleteTask(queue, taskID string) error {
	return i.inspector.DeleteTask(queue, taskID)
}

// DeleteAllArchivedTasks deletes all archived tasks in a queue
func (i *Inspector) DeleteAllArchivedTasks(queue string) (int, error) {
	return i.inspector.DeleteAllArchivedTasks(queue)
}

// ArchiveAllRetryTasks archives all retry tasks in a queue
func (i *Inspector) ArchiveAllRetryTasks(queue string) (int, error) {
	return i.inspector.ArchiveAllRetryTasks(queue)
}

// RunAllScheduledTasks runs all scheduled tasks immediately
func (i *Inspector) RunAllScheduledTasks(queue string) (int, error) {
	return i.inspector.RunAllScheduledTasks(queue)
}

// PauseQueue pauses a queue
func (i *Inspector) PauseQueue(queue string) error {
	return i.inspector.PauseQueue(queue)
}

// UnpauseQueue unpauses a queue
func (i *Inspector) UnpauseQueue(queue string) error {
	return i.inspector.UnpauseQueue(queue)
}

// convertTasks converts asynq tasks to our TaskInfo format
func convertTasks(tasks []*asynq.TaskInfo) []TaskInfo {
	result := make([]TaskInfo, len(tasks))
	for i, t := range tasks {
		result[i] = TaskInfo{
			ID:       t.ID,
			Type:     t.Type,
			Queue:    t.Queue,
			State:    t.State.String(),
			MaxRetry: t.MaxRetry,
			Retried:  t.Retried,
		}
	}
	return result
}
