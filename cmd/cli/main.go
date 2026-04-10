package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	// Global flags
	serverURL string
	tokenFile string
)

// TokenStore represents stored authentication tokens
type TokenStore struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "pandabase",
		Short: "Pandabase CLI - Vector document repository client",
		Long:  `A CLI client for interacting with Pandabase RAG knowledge base system.`,
	}

	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", "http://localhost:8080", "Pandabase server URL")
	rootCmd.PersistentFlags().StringVarP(&tokenFile, "token-file", "t", getDefaultTokenPath(), "Path to token storage file")

	// Add commands
	rootCmd.AddCommand(authCmd())
	rootCmd.AddCommand(namespaceCmd())
	rootCmd.AddCommand(documentCmd())
	rootCmd.AddCommand(searchCmd())
	rootCmd.AddCommand(configCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func getDefaultTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".pandabase_tokens.json"
	}
	return filepath.Join(home, ".pandabase", "tokens.json")
}

// Auth commands
func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication commands",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Login to Pandabase",
		RunE:  runLogin,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "logout",
		Short: "Logout and clear tokens",
		RunE:  runLogout,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Check authentication status",
		RunE:  runAuthStatus,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "register",
		Short: "Register initial admin account (only when system is uninitialized)",
		RunE:  runRegister,
	})

	return cmd
}

func runLogin(cmd *cobra.Command, args []string) error {
	fmt.Print("Email: ")
	var email string
	fmt.Scanln(&email)

	fmt.Print("Password: ")
	password := readPassword()

	payload := map[string]string{
		"email":    email,
		"password": password,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(serverURL+"/api/v1/auth/login", "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed: %s", string(body))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	tokens := TokenStore{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}

	if err := saveTokens(tokens); err != nil {
		return err
	}

	fmt.Println("✓ Login successful")
	return nil
}

func runLogout(cmd *cobra.Command, args []string) error {
	if err := os.Remove(tokenFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("✓ Logged out")
	return nil
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		fmt.Println("Not authenticated")
		return nil
	}

	// Try to get user info
	req, _ := http.NewRequest("GET", serverURL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Println("Token expired or invalid. Please login again.")
		return nil
	}

	var user struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
		Role  string `json:"role"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return err
	}

	fmt.Printf("Authenticated as: %s (%s)\n", user.Name, user.Email)
	fmt.Printf("Role: %s\n", user.Role)
	fmt.Printf("Token expires: %s\n", tokens.ExpiresAt.Format(time.RFC3339))
	return nil
}

func runRegister(cmd *cobra.Command, args []string) error {
	fmt.Print("Name: ")
	var name string
	fmt.Scanln(&name)

	fmt.Print("Email: ")
	var email string
	fmt.Scanln(&email)

	fmt.Print("Password (min 8 chars): ")
	password := readPassword()

	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	payload := map[string]string{
		"name":     name,
		"email":    email,
		"password": password,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(serverURL+"/api/v1/auth/register", "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed: %s", string(body))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	tokens := TokenStore{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}

	if err := saveTokens(tokens); err != nil {
		return err
	}

	fmt.Println("✓ Registration successful - you are now logged in as admin")
	return nil
}

// Namespace commands
func namespaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "namespace",
		Short:   "Manage namespaces (workspaces)",
		Aliases: []string{"ns"},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all namespaces",
		RunE:  runListNamespaces,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create [name]",
		Short: "Create a new namespace",
		Args:  cobra.ExactArgs(1),
		RunE:  runCreateNamespace,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a namespace",
		Args:  cobra.ExactArgs(1),
		RunE:  runDeleteNamespace,
	})

	return cmd
}

func runListNamespaces(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	req, _ := http.NewRequest("GET", serverURL+"/api/v1/namespaces", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to list namespaces: %s", string(body))
	}

	var namespaces []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		CreatedAt   string `json:"created_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&namespaces); err != nil {
		return err
	}

	if len(namespaces) == 0 {
		fmt.Println("No namespaces found")
		return nil
	}

	fmt.Printf("%-36s %-20s %s\n", "ID", "NAME", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, ns := range namespaces {
		fmt.Printf("%-36s %-20s %s\n", ns.ID, truncate(ns.Name, 20), formatTime(ns.CreatedAt))
	}

	return nil
}

func runCreateNamespace(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	name := args[0]

	payload := map[string]string{
		"name":        name,
		"description": "",
	}

	data, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", serverURL+"/api/v1/namespaces", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create namespace: %s", string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("✓ Created namespace '%s' (ID: %s)\n", name, result.ID)
	return nil
}

func runDeleteNamespace(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	id := args[0]

	req, _ := http.NewRequest("DELETE", serverURL+"/api/v1/namespaces/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete namespace: %s", string(body))
	}

	fmt.Println("✓ Namespace deleted")
	return nil
}

// Document commands
func documentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "document",
		Short:   "Manage documents",
		Aliases: []string{"doc", "docs"},
	}

	listCmd := &cobra.Command{
		Use:   "list [namespace-id]",
		Short: "List documents in a namespace",
		Args:  cobra.ExactArgs(1),
		RunE:  runListDocuments,
	}
	listCmd.Flags().String("status", "", "Filter by status (pending, processing, completed, failed)")
	cmd.AddCommand(listCmd)

	uploadCmd := &cobra.Command{
		Use:   "upload [namespace-id] [file-path]",
		Short: "Upload a document",
		Args:  cobra.ExactArgs(2),
		RunE:  runUploadDocument,
	}
	uploadCmd.Flags().Int("chunk-size", 500, "Chunk size")
	uploadCmd.Flags().Int("chunk-overlap", 50, "Chunk overlap")
	cmd.AddCommand(uploadCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete [namespace-id] [document-id]",
		Short: "Delete a document",
		Args:  cobra.ExactArgs(2),
		RunE:  runDeleteDocument,
	})

	downloadCmd := &cobra.Command{
		Use:   "download [namespace-id] [document-id] [output-path]",
		Short: "Download a document",
		Args:  cobra.ExactArgs(3),
		RunE:  runDownloadDocument,
	}
	cmd.AddCommand(downloadCmd)

	importCmd := &cobra.Command{
		Use:   "import [namespace-id] [url]",
		Short: "Import a document from URL",
		Args:  cobra.ExactArgs(2),
		RunE:  runImportURL,
	}
	importCmd.Flags().String("parser", "web", "Parser type (web, notion)")
	importCmd.Flags().Int("chunk-size", 1000, "Chunk size")
	importCmd.Flags().Int("chunk-overlap", 100, "Chunk overlap")
	importCmd.Flags().Bool("render", false, "Render JavaScript")
	cmd.AddCommand(importCmd)

	batchImportCmd := &cobra.Command{
		Use:   "batch-import [namespace-id] [file-path]",
		Short: "Batch import documents from a list of URLs in a file",
		Args:  cobra.ExactArgs(2),
		RunE:  runBatchImport,
	}
	batchImportCmd.Flags().String("parser", "web", "Parser type (web, notion)")
	batchImportCmd.Flags().Int("concurrency", 5, "Number of concurrent imports")
	cmd.AddCommand(batchImportCmd)

	return cmd
}

func runListDocuments(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	nsID := args[0]
	status, _ := cmd.Flags().GetString("status")

	url := serverURL + "/api/v1/namespaces/" + nsID + "/documents"
	if status != "" {
		url += "?status=" + status
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to list documents: %s", string(body))
	}

	var result struct {
		Data []struct {
			ID         string                 `json:"id"`
			SourceType string                 `json:"source_type"`
			SourceURI  string                 `json:"source_uri"`
			Status     string                 `json:"status"`
			CreatedAt  string                 `json:"created_at"`
			Metadata   map[string]interface{} `json:"metadata"`
		} `json:"data"`
		Total int64 `json:"total"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if len(result.Data) == 0 {
		fmt.Println("No documents found")
		return nil
	}

	fmt.Printf("%-36s %-15s %-12s %-20s %s\n", "ID", "TYPE", "STATUS", "FILENAME", "CREATED")
	fmt.Println(strings.Repeat("-", 100))
	for _, doc := range result.Data {
		filename := "unknown"
		if fn, ok := doc.Metadata["original_filename"].(string); ok {
			filename = fn
		}
		fmt.Printf("%-36s %-15s %-12s %-20s %s\n",
			doc.ID,
			doc.SourceType,
			doc.Status,
			truncate(filename, 20),
			formatTime(doc.CreatedAt),
		)
	}

	fmt.Printf("\nTotal: %d documents\n", result.Total)
	return nil
}

func runUploadDocument(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	nsID := args[0]
	filePath := args[1]

	chunkSize, _ := cmd.Flags().GetInt("chunk-size")
	chunkOverlap, _ := cmd.Flags().GetInt("chunk-overlap")

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Build multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return err
	}
	io.Copy(part, file)

	writer.WriteField("chunk_size", strconv.Itoa(chunkSize))
	writer.WriteField("chunk_overlap", strconv.Itoa(chunkOverlap))
	writer.Close()

	req, _ := http.NewRequest("POST", serverURL+"/api/v1/namespaces/"+nsID+"/documents", &body)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	fmt.Printf("Uploading %s...\n", filepath.Base(filePath))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed: %s", string(body))
	}

	var result struct {
		DocumentID string `json:"document_id"`
		Status     string `json:"status"`
		TaskID     string `json:"task_id"`
		Message    string `json:"message"`
	}

	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("✓ Uploaded successfully\n")
	fmt.Printf("  Document ID: %s\n", result.DocumentID)
	fmt.Printf("  Task ID: %s\n", result.TaskID)
	fmt.Printf("  Status: %s\n", result.Status)
	return nil
}

func runDeleteDocument(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	nsID := args[0]
	docID := args[1]

	req, _ := http.NewRequest("DELETE", serverURL+"/api/v1/namespaces/"+nsID+"/documents/"+docID, nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete document: %s", string(body))
	}

	fmt.Println("✓ Document deletion queued")
	return nil
}

func runDownloadDocument(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	nsID := args[0]
	docID := args[1]
	outputPath := args[2]

	req, _ := http.NewRequest("GET", serverURL+"/api/v1/namespaces/"+nsID+"/documents/"+docID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to download document: %s", string(body))
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	size, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Downloaded %s (%d bytes)\n", outputPath, size)
	return nil
}

func runImportURL(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	nsID := args[0]
	url := args[1]

	parser, _ := cmd.Flags().GetString("parser")
	chunkSize, _ := cmd.Flags().GetInt("chunk-size")
	chunkOverlap, _ := cmd.Flags().GetInt("chunk-overlap")
	render, _ := cmd.Flags().GetBool("render")

	return performImport(tokens.AccessToken, nsID, url, parser, chunkSize, chunkOverlap, render)
}

func performImport(token, nsID, url, parser string, chunkSize, chunkOverlap int, render bool) error {
	payload := map[string]interface{}{
		"url":               url,
		"parser_type":       parser,
		"chunk_size":        chunkSize,
		"chunk_overlap":     chunkOverlap,
		"render_javascript": render,
	}

	data, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", serverURL+"/api/v1/namespaces/"+nsID+"/documents/import", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("import failed for %s: %s", url, string(body))
	}

	var result struct {
		DocumentID string `json:"document_id"`
		TaskID     string `json:"task_id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("✓ Queued import: %s (Doc ID: %s, Task ID: %s)\n", url, result.DocumentID, result.TaskID)
	return nil
}

func runBatchImport(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	nsID := args[0]
	filePath := args[1]
	parser, _ := cmd.Flags().GetString("parser")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	urls := strings.Split(string(content), "\n")
	var validURLs []string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u != "" && !strings.HasPrefix(u, "#") {
			validURLs = append(validURLs, u)
		}
	}

	if len(validURLs) == 0 {
		fmt.Println("No valid URLs found in file")
		return nil
	}

	fmt.Printf("Batch importing %d URLs with concurrency %d...\n", len(validURLs), concurrency)

	sem := make(chan struct{}, concurrency)
	for _, u := range validURLs {
		sem <- struct{}{}
		go func(url string) {
			defer func() { <-sem }()
			if err := performImport(tokens.AccessToken, nsID, url, parser, 1000, 100, false); err != nil {
				fmt.Printf("✗ Failed import: %s - %v\n", url, err)
			}
		}(u)
	}

	// Wait for all workers
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	fmt.Println("✓ Batch import process finished")
	return nil
}

// Search commands
func searchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search documents using vector similarity",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "query [namespace-id] [query]",
		Short: "Search for similar content",
		Args:  cobra.MinimumNArgs(2),
		RunE:  runSearch,
	})

	return cmd
}

func runSearch(cmd *cobra.Command, args []string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'pandabase auth login'")
	}

	nsID := args[0]
	query := strings.Join(args[1:], " ")

	payload := map[string]interface{}{
		"namespace_ids": []string{nsID},
		"query":         query,
		"top_k":         5,
	}

	data, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", serverURL+"/api/v1/search", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("search failed: %s", string(body))
	}

	var results []struct {
		Score float64 `json:"score"`
		Chunk struct {
			Content  string                 `json:"content"`
			Metadata map[string]interface{} `json:"metadata"`
		} `json:"chunk"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("No results found")
		return nil
	}

	fmt.Printf("Found %d results:\n\n", len(results))
	for i, r := range results {
		fmt.Printf("[%d] Score: %.4f\n", i+1, r.Score)
		if filename, ok := r.Chunk.Metadata["file_name"].(string); ok {
			fmt.Printf("    Source: %s\n", filename)
		}
		fmt.Printf("    %s\n\n", truncate(r.Chunk.Content, 200))
	}

	return nil
}

// Config commands
func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set-server [url]",
		Short: "Set default server URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			// Save to config file
			configPath := getConfigPath()
			config := map[string]string{
				"server_url": url,
			}
			data, _ := json.MarshalIndent(config, "", "  ")
			os.MkdirAll(filepath.Dir(configPath), 0755)
			if err := os.WriteFile(configPath, data, 0600); err != nil {
				return err
			}
			fmt.Printf("✓ Default server URL set to: %s\n", url)
			return nil
		},
	})

	return cmd
}

// Helper functions
func loadTokens() (TokenStore, error) {
	var tokens TokenStore
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return tokens, err
	}
	if err := json.Unmarshal(data, &tokens); err != nil {
		return tokens, err
	}
	return tokens, nil
}

func saveTokens(tokens TokenStore) error {
	os.MkdirAll(filepath.Dir(tokenFile), 0755)
	data, _ := json.MarshalIndent(tokens, "", "  ")
	return os.WriteFile(tokenFile, data, 0600)
}

func getConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".pandabase_config.json"
	}
	return filepath.Join(home, ".pandabase", "config.json")
}

func readPassword() string {
	// Simple password reading (no echo)
	// In production, use golang.org/x/term for proper terminal handling
	var password string
	fmt.Scanln(&password)
	return password
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func formatTime(t string) string {
	// Try to parse and format time
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
	} {
		if parsed, err := time.Parse(layout, t); err == nil {
			return parsed.Format("2006-01-02 15:04")
		}
	}
	return t
}
