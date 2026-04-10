package postprocess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewService(t *testing.T) {
	tests := []struct {
		name         string
		apiURL       string
		apiKey       string
		model        string
		customPrompt string
		enabled      bool
		wantEnabled  bool
	}{
		{
			name:         "disabled when enabled=false",
			apiURL:       "https://api.openai.com/v1",
			apiKey:       "test-key",
			model:        "gpt-4o-mini",
			customPrompt: "",
			enabled:      false,
			wantEnabled:  false,
		},
		{
			name:         "disabled when apiKey is empty",
			apiURL:       "https://api.openai.com/v1",
			apiKey:       "",
			model:        "gpt-4o-mini",
			customPrompt: "",
			enabled:      true,
			wantEnabled:  false,
		},
		{
			name:         "enabled with valid config",
			apiURL:       "https://api.openai.com/v1",
			apiKey:       "test-key",
			model:        "gpt-4o-mini",
			customPrompt: "",
			enabled:      true,
			wantEnabled:  true,
		},
		{
			name:         "uses default values",
			apiURL:       "",
			apiKey:       "test-key",
			model:        "",
			customPrompt: "",
			enabled:      true,
			wantEnabled:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(tt.apiURL, tt.apiKey, tt.model, tt.customPrompt, tt.enabled)
			if svc.IsEnabled() != tt.wantEnabled {
				t.Errorf("IsEnabled() = %v, want %v", svc.IsEnabled(), tt.wantEnabled)
			}
		})
	}
}

func TestService_Process_Disabled(t *testing.T) {
	svc := NewService("", "", "", "", false)
	
	input := "some raw web content"
	result, err := svc.Process(context.Background(), input)
	
	if err != nil {
		t.Errorf("Process() error = %v", err)
	}
	if result != input {
		t.Errorf("Process() = %v, want %v", result, input)
	}
}

func TestService_Process_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("Missing or invalid Authorization header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Missing or invalid Content-Type header")
		}

		response := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "Cleaned content",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	svc := NewService(server.URL, "test-key", "gpt-4o-mini", "", true)
	
	input := "some raw web content"
	result, err := svc.Process(context.Background(), input)
	
	if err != nil {
		t.Errorf("Process() error = %v", err)
	}
	if result != "Cleaned content" {
		t.Errorf("Process() = %v, want %v", result, "Cleaned content")
	}
}

func TestService_Process_EmptyContent(t *testing.T) {
	svc := NewService("https://api.openai.com/v1", "test-key", "gpt-4o-mini", "", true)
	
	result, err := svc.Process(context.Background(), "")
	
	if err != nil {
		t.Errorf("Process() error = %v", err)
	}
	if result != "" {
		t.Errorf("Process() = %v, want empty string", result)
	}
}

func TestService_Validate(t *testing.T) {
	tests := []struct {
		name    string
		svc     *Service
		wantErr bool
	}{
		{
			name:    "disabled service is valid",
			svc:     NewService("", "", "", "", false),
			wantErr: false,
		},
		{
			name:    "enabled with all fields",
			svc:     NewService("https://api.openai.com/v1", "key", "model", "", true),
			wantErr: false,
		},
		{
			name:    "enabled missing apiURL uses default",
			svc:     NewService("", "key", "model", "", true),
			wantErr: false, // Uses default API URL
		},
		{
			name:    "enabled missing apiKey is auto-disabled",
			svc:     NewService("https://api.openai.com/v1", "", "model", "", true),
			wantErr: false, // Auto-disabled when apiKey is empty
		},
		{
			name:    "enabled missing model uses default",
			svc:     NewService("https://api.openai.com/v1", "key", "", "", true),
			wantErr: false, // Uses default model
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.svc.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestService_Process_APIError(t *testing.T) {
	// Create a mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer server.Close()

	svc := NewService(server.URL, "invalid-key", "gpt-4o-mini", "", true)
	
	_, err := svc.Process(context.Background(), "some content")
	
	if err == nil {
		t.Error("Process() expected error, got nil")
	}
}

func TestService_Process_APIReturnsError(t *testing.T) {
	// Create a mock server that returns an error in the response body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"error": map[string]string{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	svc := NewService(server.URL, "test-key", "gpt-4o-mini", "", true)
	
	_, err := svc.Process(context.Background(), "some content")
	
	if err == nil {
		t.Error("Process() expected error, got nil")
	}
}

func TestService_Process_NoChoices(t *testing.T) {
	// Create a mock server that returns empty choices
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"choices": []map[string]interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	svc := NewService(server.URL, "test-key", "gpt-4o-mini", "", true)
	
	_, err := svc.Process(context.Background(), "some content")
	
	if err == nil {
		t.Error("Process() expected error, got nil")
	}
}

func TestDefaultPrompt(t *testing.T) {
	if DefaultPrompt == "" {
		t.Error("DefaultPrompt should not be empty")
	}
	
	// Check that the prompt contains key instructions
	keyPhrases := []string{
		"navigation menus",
		"headers",
		"footers",
		"advertisements",
		"cookie notices",
		"main article",
		"markdown",
	}
	
	for _, phrase := range keyPhrases {
		if !contains(DefaultPrompt, phrase) {
			t.Errorf("DefaultPrompt should contain '%s'", phrase)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	if start+len(substr) > len(s) {
		return false
	}
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
