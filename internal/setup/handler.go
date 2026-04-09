package setup

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"pandabase/internal/config"
)

type SetupHandler struct {
	configPath string
}

func NewSetupHandler(configPath string) *SetupHandler {
	return &SetupHandler{configPath: configPath}
}

func (h *SetupHandler) RegisterRoutes(router *gin.Engine) {
	router.GET("/setup", h.ServeSetupPage)
	router.POST("/api/v1/setup", h.HandleSetup)
}

func (h *SetupHandler) ServeSetupPage(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, setupHTML)
}

func (h *SetupHandler) HandleSetup(c *gin.Context) {
	var cfg config.Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Basic validation
	if cfg.Embedding.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API Key is required"})
		return
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal config"})
		return
	}

	if err := os.WriteFile(h.configPath, data, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write config file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Setup completed successfully. Restarting server..."})

	// Exit the process so Docker can restart it with the new configuration
	go func() {
		fmt.Println("Setup completed. Exiting process for restart...")
		os.Exit(0)
	}()
}

const setupHTML = `
<!DOCTYPE html>
<html>
<head>
    <title>Pandabase Setup Wizard</title>
    <style>
        body { font-family: sans-serif; max-width: 600px; margin: 40px auto; padding: 20px; line-height: 1.6; }
        .form-group { margin-bottom: 15px; }
        label { display: block; margin-bottom: 5px; font-weight: bold; }
        input, select { width: 100%; padding: 8px; box-sizing: border-box; }
        button { background: #4caf50; color: white; padding: 10px 15px; border: none; cursor: pointer; border-radius: 4px; font-size: 16px; }
        button:hover { background: #45a049; }
        .card { border: 1px solid #ddd; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        h1 { color: #333; }
        .error { color: red; margin-top: 10px; display: none; }
        .success { color: green; margin-top: 10px; display: none; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Pandabase Setup</h1>
        <p>Please complete the configuration to initialize your RAG system.</p>
        
        <div class="form-group">
            <label>Embedding API Provider</label>
            <select id="api_url" onchange="onProviderChange()">
                <option value="https://openrouter.ai/api/v1">OpenRouter (Recommended)</option>
                <option value="https://api.openai.com/v1">OpenAI</option>
                <option value="https://ark.cn-beijing.volces.com/api/v3">Doubao/Volcano</option>
                <option value="custom">Custom</option>
            </select>
        </div>

        <div class="form-group" id="custom_url_group" style="display:none">
            <label>Custom API URL</label>
            <input type="text" id="custom_api_url" placeholder="https://your-api-endpoint.com/v1">
        </div>

        <div class="form-group">
            <label>Model Name</label>
            <input type="text" id="model" value="qwen/qwen3-embedding-8b">
        </div>

        <div class="form-group">
            <label>Dimensions</label>
            <input type="number" id="dimensions" value="4096">
        </div>

        <div class="form-group">
            <label>API Key</label>
            <input type="password" id="api_key" placeholder="sk-...">
        </div>

        <hr>

        <div class="form-group">
            <label>Database Host</label>
            <input type="text" id="db_host" value="postgres">
        </div>

        <div class="form-group">
            <label>Use Half-precision (Halfvec)</label>
            <select id="use_halfvec">
                <option value="false">No (Standard)</option>
                <option value="true">Yes (For > 2000 dimensions)</option>
            </select>
        </div>

        <button onclick="submitSetup()">Finish Setup</button>
        <div id="error" class="error"></div>
        <div id="success" class="success"></div>
    </div>

    <script>
        function onProviderChange() {
            const provider = document.getElementById('api_url').value;
            const customGroup = document.getElementById('custom_url_group');
            if (provider === 'custom') {
                customGroup.style.display = 'block';
            } else {
                customGroup.style.display = 'none';
            }
        }

        async function submitSetup() {
            const errorDiv = document.getElementById('error');
            const successDiv = document.getElementById('success');
            errorDiv.style.display = 'none';
            
            const provider = document.getElementById('api_url').value;
            let apiUrl = provider;
            if (provider === 'custom') {
                apiUrl = document.getElementById('custom_api_url').value;
                if (!apiUrl) {
                    errorDiv.innerText = 'Custom API URL is required when using Custom provider';
                    errorDiv.style.display = 'block';
                    return;
                }
            }

            const payload = {
                database: {
                    host: document.getElementById('db_host').value,
                    port: "5432",
                    user: "pandabase",
                    password: "pandabase",
                    name: "pandabase",
                    ssl_mode: "disable",
                    use_halfvec: document.getElementById('use_halfvec').value === 'true'
                },
                redis: {
                    host: "redis",
                    port: "6379",
                    db: 0
                },
                storage: {
                    type: "filesystem",
                    data_path: "./data/files",
                    max_file_size: 100
                },
                server: {
                    host: "0.0.0.0",
                    port: "8080"
                },
                log: {
                    level: "info",
                    format: "json"
                },
                embedding: {
                    api_url: apiUrl,
                    model: document.getElementById('model').value,
                    dimensions: parseInt(document.getElementById('dimensions').value),
                    api_key: document.getElementById('api_key').value,
                    enable_multimodal: false
                },
                auth: {
                    jwt_expiry: "24h",
                    refresh_token_expiry: "168h",
                    enable_oauth: false
                }
            };

            try {
                const resp = await fetch('/api/v1/setup', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                const data = await resp.json();
                if (resp.ok) {
                    successDiv.innerText = data.message;
                    successDiv.style.display = 'block';
                    setTimeout(() => window.location.href = '/', 3000);
                } else {
                    errorDiv.innerText = data.error || 'Setup failed';
                    errorDiv.style.display = 'block';
                }
            } catch (e) {
                errorDiv.innerText = 'Connection error: ' + e.message;
                errorDiv.style.display = 'block';
            }
        }
    </script>
</body>
</html>
`
