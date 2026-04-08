package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	Database  DatabaseConfig  `mapstructure:"database"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Server    ServerConfig    `mapstructure:"server"`
	Log       LogConfig       `mapstructure:"log"`
	Embedding EmbeddingConfig `mapstructure:"embedding"`
	Auth      AuthConfig      `mapstructure:"auth"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host          string `mapstructure:"host"`
	Port          string `mapstructure:"port"`
	User          string `mapstructure:"user"`
	Password      string `mapstructure:"password"`
	Name          string `mapstructure:"name"`
	SSLMode       string `mapstructure:"ssl_mode"`
	LogLevel      string `mapstructure:"log_level"`
	FTSDictionary string `mapstructure:"fts_dictionary"` // Full-text search dictionary, e.g., 'simple', 'jieba', 'zhcn'
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// StorageConfig holds file storage configuration
type StorageConfig struct {
	Type       string `mapstructure:"type"`        // "filesystem" or "memory"
	DataPath   string `mapstructure:"data_path"`   // Path for filesystem storage
	MaxFileSize int64 `mapstructure:"max_file_size"` // Max file size in MB
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port string `mapstructure:"port"`
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// EmbeddingConfig holds embedding service configuration
type EmbeddingConfig struct {
	Model                string `mapstructure:"model"`                // Text embedding model
	MultimodalModel      string `mapstructure:"multimodal_model"`       // Multimodal embedding model (optional)
	Dimensions           int    `mapstructure:"dimensions"`             // Text model dimensions
	MultimodalDimensions int    `mapstructure:"multimodal_dimensions"`  // Multimodal model dimensions (if different)
	APIURL               string `mapstructure:"api_url"`
	APIKey               string `mapstructure:"api_key"`
	EnableMultimodal     bool   `mapstructure:"enable_multimodal"`      // Enable multimodal embeddings
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	JWTSecret          string `mapstructure:"jwt_secret"`
	JWTExpiry          string `mapstructure:"jwt_expiry"`
	RefreshTokenExpiry string `mapstructure:"refresh_token_expiry"`
	EnableOAuth        bool   `mapstructure:"enable_oauth"`
	OAuthProviders     struct {
		Google struct {
			Enabled      bool   `mapstructure:"enabled"`
			ClientID     string `mapstructure:"client_id"`
			ClientSecret string `mapstructure:"client_secret"`
			RedirectURL  string `mapstructure:"redirect_url"`
		} `mapstructure:"google"`
		GitHub struct {
			Enabled      bool   `mapstructure:"enabled"`
			ClientID     string `mapstructure:"client_id"`
			ClientSecret string `mapstructure:"client_secret"`
			RedirectURL  string `mapstructure:"redirect_url"`
		} `mapstructure:"github"`
	} `mapstructure:"oauth_providers"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Embedding.Dimensions <= 0 {
		return fmt.Errorf("embedding dimensions must be positive, got %d", c.Embedding.Dimensions)
	}
	if c.Embedding.Dimensions > 8192 {
		return fmt.Errorf("embedding dimensions too large (max 8192), got %d", c.Embedding.Dimensions)
	}
	return nil
}

// Load loads configuration from file and environment variables
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read from config file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Read from environment variables
	v.SetEnvPrefix("PANDABASE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", "5432")
	v.SetDefault("database.user", "pandabase")
	v.SetDefault("database.password", "pandabase")
	v.SetDefault("database.name", "pandabase")
	v.SetDefault("database.ssl_mode", "disable")
	v.SetDefault("database.log_level", "error")
	v.SetDefault("database.fts_dictionary", "simple")

	// Redis defaults
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", "6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)

	// Storage defaults
	v.SetDefault("storage.type", "filesystem")
	v.SetDefault("storage.data_path", "./data/files")
	v.SetDefault("storage.max_file_size", 100) // 100 MB

	// Server defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", "8080")

	// Log defaults
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	// Embedding defaults
	v.SetDefault("embedding.model", "text-embedding-ada-002")
	v.SetDefault("embedding.multimodal_model", "")
	v.SetDefault("embedding.dimensions", 1536)
	v.SetDefault("embedding.multimodal_dimensions", 0)
	v.SetDefault("embedding.api_url", "https://api.openai.com/v1")
	v.SetDefault("embedding.enable_multimodal", false)

	// Auth defaults
	v.SetDefault("auth.jwt_expiry", "24h")
	v.SetDefault("auth.refresh_token_expiry", "168h")
	v.SetDefault("auth.enable_oauth", false)
}
