package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	baseURL       = "http://localhost:8080"
	testEmail     = "smoke@test.com"
	testPassword  = "password123"
	testName      = "Smoke Test User"
	testNamespace = "smoke-test-ns"
)

var (
	authToken     string
	refreshToken  string
	namespaceID   string
	documentID    string
	httpClient    = &http.Client{Timeout: 30 * time.Second}
)

// Helper: make HTTP request
func doRequest(method, path string, body interface{}, token string) (*http.Response, map[string]interface{}) {
	var reqBody io.Reader
	if body != nil {
		switch v := body.(type) {
		case io.Reader:
			reqBody = v
		default:
			b, _ := json.Marshal(body)
			reqBody = bytes.NewReader(b)
		}
	}

	req, _ := http.NewRequest(method, baseURL+path, reqBody)
	if _, ok := body.(io.Reader); !ok && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, map[string]interface{}{"error": err.Error()}
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	return resp, result
}

// Helper: create multipart file upload request
func doFileUpload(path, token string, fileContent, filename string, extraFields map[string]string) (*http.Response, map[string]interface{}) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, _ := writer.CreateFormFile("file", filename)
	part.Write([]byte(fileContent))

	for key, val := range extraFields {
		writer.WriteField(key, val)
	}
	writer.Close()

	req, _ := http.NewRequest("POST", baseURL+path, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, map[string]interface{}{"error": err.Error()}
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	return resp, result
}

func TestMain(m *testing.M) {
	// Wait for server to be ready
	fmt.Println("Waiting for server to be ready...")
	for i := 0; i < 30; i++ {
		resp, _ := http.Get(baseURL + "/health")
		if resp != nil && resp.StatusCode == 200 {
			resp.Body.Close()
			fmt.Println("Server is ready!")
			break
		}
		if i == 29 {
			fmt.Println("Server not ready after 30 seconds, aborting tests")
			os.Exit(1)
		}
		time.Sleep(1 * time.Second)
	}

	code := m.Run()
	os.Exit(code)
}

// ========== Health Check ==========

func TestHealthCheck(t *testing.T) {
	resp, result := doRequest("GET", "/health", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
	if result["status"] != "ok" {
		t.Fatalf("Expected status ok, got %v", result["status"])
	}
}

// ========== Auth: Register ==========

func TestAuthRegister(t *testing.T) {
	body := map[string]string{
		"email":    testEmail,
		"password": testPassword,
		"name":     testName,
	}
	resp, result := doRequest("POST", "/api/v1/auth/register", body, "")
	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		t.Fatalf("Expected 201 or 409 (duplicate), got %d: %v", resp.StatusCode, result)
	}

	// If 409, user already exists — try login instead
	if resp.StatusCode == 409 {
		t.Log("User already exists, proceeding to login")
		return
	}

	if result["access_token"] == nil {
		t.Fatal("Expected access_token in response")
	}
	if result["refresh_token"] == nil {
		t.Fatal("Expected refresh_token in response")
	}

	authToken = result["access_token"].(string)
	refreshToken = result["refresh_token"].(string)
}

// ========== Auth: Login ==========

func TestAuthLogin(t *testing.T) {
	body := map[string]string{
		"email":    testEmail,
		"password": testPassword,
	}
	resp, result := doRequest("POST", "/api/v1/auth/login", body, "")
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}

	if result["access_token"] == nil {
		t.Fatal("Expected access_token in response")
	}

	authToken = result["access_token"].(string)
	refreshToken = result["refresh_token"].(string)
}

// ========== Auth: Get Me ==========

func TestAuthGetMe(t *testing.T) {
	resp, result := doRequest("GET", "/api/v1/auth/me", nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
	if result["email"] != testEmail {
		t.Fatalf("Expected email %s, got %v", testEmail, result["email"])
	}
}

// ========== Auth: Refresh Token ==========

func TestAuthRefreshToken(t *testing.T) {
	// Re-login to get fresh tokens
	body := map[string]string{
		"email":    testEmail,
		"password": testPassword,
	}
	_, loginResult := doRequest("POST", "/api/v1/auth/login", body, "")
	freshRefreshToken := loginResult["refresh_token"].(string)

	refreshBody := map[string]string{
		"refresh_token": freshRefreshToken,
	}
	resp, result := doRequest("POST", "/api/v1/auth/refresh", refreshBody, "")
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
	if result["access_token"] == nil {
		t.Fatal("Expected new access_token")
	}
	authToken = result["access_token"].(string)
	refreshToken = result["refresh_token"].(string)
}

// ========== Auth: Get Providers ==========

func TestAuthGetProviders(t *testing.T) {
	resp, result := doRequest("GET", "/api/v1/auth/providers", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Auth: Unauthorized access ==========

func TestAuthUnauthorizedAccess(t *testing.T) {
	resp, _ := doRequest("GET", "/api/v1/auth/me", nil, "")
	if resp.StatusCode != 401 {
		t.Fatalf("Expected 401 for unauthenticated request, got %d", resp.StatusCode)
	}
}

// ========== Namespace: Create ==========

func TestNamespaceCreate(t *testing.T) {
	uniqueName := fmt.Sprintf("%s-%d", testNamespace, time.Now().UnixNano())
	body := map[string]string{
		"name":        uniqueName,
		"description": "Namespace for smoke testing",
	}
	resp, result := doRequest("POST", "/api/v1/namespaces", body, authToken)
	if resp.StatusCode != 201 {
		t.Fatalf("Expected 201, got %d: %v", resp.StatusCode, result)
	}

	id, ok := result["id"].(string)
	if !ok || id == "" {
		t.Fatal("Expected namespace id in response")
	}
	namespaceID = id
}

// ========== Namespace: List ==========

func TestNamespaceList(t *testing.T) {
	resp, result := doRequest("GET", "/api/v1/namespaces", nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Namespace: Get ==========

func TestNamespaceGet(t *testing.T) {
	if namespaceID == "" {
		t.Skip("No namespace ID available")
	}
	resp, result := doRequest("GET", "/api/v1/namespaces/"+namespaceID, nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
	if result["name"] == nil || result["name"] == "" {
		t.Fatal("Expected name in response")
	}
}

// ========== Namespace: Update ==========

func TestNamespaceUpdate(t *testing.T) {
	if namespaceID == "" {
		t.Skip("No namespace ID available")
	}
	newName := fmt.Sprintf("%s-updated-%d", testNamespace, time.Now().UnixNano())
	body := map[string]string{
		"name":        newName,
		"description": "Updated description",
	}
	resp, result := doRequest("PUT", "/api/v1/namespaces/"+namespaceID, body, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Document: Upload ==========

func TestDocumentUpload(t *testing.T) {
	if namespaceID == "" {
		t.Skip("No namespace ID available")
	}

	content := "# Test Document\n\nThis is a smoke test document.\n\n## Section 1\n\nSome content here."
	path := fmt.Sprintf("/api/v1/namespaces/%s/documents", namespaceID)

	resp, result := doFileUpload(path, authToken, content, "test.md", map[string]string{
		"chunk_size":    "500",
		"chunk_overlap": "50",
	})

	if resp.StatusCode != 201 {
		t.Fatalf("Expected 201, got %d: %v", resp.StatusCode, result)
	}

	id, ok := result["id"].(string)
	if !ok || id == "" {
		// Try document_id field
		id, ok = result["document_id"].(string)
	}
	if ok && id != "" {
		documentID = id
	}
}

// ========== Document: List ==========

func TestDocumentList(t *testing.T) {
	if namespaceID == "" {
		t.Skip("No namespace ID available")
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/documents", namespaceID)
	resp, result := doRequest("GET", path, nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Document: Get ==========

func TestDocumentGet(t *testing.T) {
	if namespaceID == "" || documentID == "" {
		t.Skip("No namespace or document ID available")
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/documents/%s", namespaceID, documentID)
	resp, result := doRequest("GET", path, nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Document: Download ==========

func TestDocumentDownload(t *testing.T) {
	if namespaceID == "" || documentID == "" {
		t.Skip("No namespace or document ID available")
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/documents/%s/download", namespaceID, documentID)
	resp, _ := doRequest("GET", path, nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
}

// ========== Queue: Stats ==========

func TestQueueStats(t *testing.T) {
	resp, result := doRequest("GET", "/api/v1/queue/stats", nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Queue: List Tasks ==========

func TestQueueListTasks(t *testing.T) {
	resp, result := doRequest("GET", "/api/v1/queue/tasks?queue=default&state=pending", nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Search ==========

func TestSearch(t *testing.T) {
	body := map[string]interface{}{
		"query":           "smoke test",
		"top_k":           5,
		"mode":            "fulltext",
		"include_content": true,
	}
	resp, result := doRequest("POST", "/api/v1/search", body, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Search: Invalid Request ==========

func TestSearchInvalidRequest(t *testing.T) {
	body := map[string]interface{}{
		"query": "",
		"top_k": 0,
	}
	resp, _ := doRequest("POST", "/api/v1/search", body, authToken)
	if resp.StatusCode != 400 {
		t.Fatalf("Expected 400 for invalid search request, got %d", resp.StatusCode)
	}
}

// ========== Chunks: Get Document Chunks ==========

func TestGetDocumentChunks(t *testing.T) {
	if documentID == "" {
		t.Skip("No document ID available")
	}
	path := fmt.Sprintf("/api/v1/documents/%s/chunks", documentID)
	resp, result := doRequest("GET", path, nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Document: Delete ==========

func TestDocumentDelete(t *testing.T) {
	if namespaceID == "" || documentID == "" {
		t.Skip("No namespace or document ID available")
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/documents/%s?cascade=true", namespaceID, documentID)
	resp, result := doRequest("DELETE", path, nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
}

// ========== Namespace: Delete ==========

func TestNamespaceDelete(t *testing.T) {
	if namespaceID == "" {
		t.Skip("No namespace ID available")
	}
	resp, result := doRequest("DELETE", "/api/v1/namespaces/"+namespaceID, nil, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d: %v", resp.StatusCode, result)
	}
	namespaceID = ""
}

// ========== Auth: Duplicate Registration ==========

func TestAuthDuplicateRegistration(t *testing.T) {
	body := map[string]string{
		"email":    testEmail,
		"password": testPassword,
		"name":     testName,
	}
	resp, _ := doRequest("POST", "/api/v1/auth/register", body, "")
	if resp.StatusCode != 409 {
		t.Fatalf("Expected 409 for duplicate registration, got %d", resp.StatusCode)
	}
}

// ========== Auth: Invalid Credentials ==========

func TestAuthInvalidCredentials(t *testing.T) {
	body := map[string]string{
		"email":    testEmail,
		"password": "wrongpassword",
	}
	resp, _ := doRequest("POST", "/api/v1/auth/login", body, "")
	if resp.StatusCode != 401 {
		t.Fatalf("Expected 401 for invalid credentials, got %d", resp.StatusCode)
	}
}

// ========== Full E2E: Upload + Search ==========

func TestE2EUploadAndSearch(t *testing.T) {
	// Create a new namespace for this test
	nsBody := map[string]string{
		"name":        fmt.Sprintf("e2e-search-test-%d", time.Now().UnixNano()),
		"description": "E2E search test namespace",
	}
	resp, nsResult := doRequest("POST", "/api/v1/namespaces", nsBody, authToken)
	if resp.StatusCode != 201 {
		t.Fatalf("Failed to create namespace: %d %v", resp.StatusCode, nsResult)
	}
	nsID := nsResult["id"].(string)
	defer doRequest("DELETE", "/api/v1/namespaces/"+nsID, nil, authToken)

	// Upload a document
	content := strings.Repeat("Pandabase is a powerful RAG knowledge base system that supports vector search and full-text search. ", 10)
	path := fmt.Sprintf("/api/v1/namespaces/%s/documents", nsID)
	resp, uploadResult := doFileUpload(path, authToken, content, "e2e_test.txt", map[string]string{
		"chunk_size": "200",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("Failed to upload document: %d %v", resp.StatusCode, uploadResult)
	}
	docID, _ := uploadResult["id"].(string)

	// Wait for async processing
	t.Log("Waiting for document processing...")
	time.Sleep(5 * time.Second)

	// Check document status
	if docID != "" {
		docPath := fmt.Sprintf("/api/v1/namespaces/%s/documents/%s", nsID, docID)
		_, docResult := doRequest("GET", docPath, nil, authToken)
		t.Logf("Document status: %v", docResult["status"])
	}

	// Try fulltext search (doesn't need embeddings)
	searchBody := map[string]interface{}{
		"query":           "Pandabase RAG",
		"namespace_ids":   []string{nsID},
		"top_k":           5,
		"mode":            "fulltext",
		"include_content": true,
	}
	resp, searchResult := doRequest("POST", "/api/v1/search", searchBody, authToken)
	if resp.StatusCode != 200 {
		t.Fatalf("Search failed: %d %v", resp.StatusCode, searchResult)
	}
	t.Logf("Search returned %v results", searchResult["total_count"])
}
