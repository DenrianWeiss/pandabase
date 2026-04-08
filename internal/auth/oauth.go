package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
	"gorm.io/gorm"

	"pandabase/internal/db/models"
)

// OAuthService handles OAuth authentication
type OAuthService struct {
	db       *gorm.DB
	authSvc  *Service
	configs  map[string]*oauth2.Config
}

// NewOAuthService creates a new OAuth service
func NewOAuthService(db *gorm.DB, authSvc *Service, cfg Config) *OAuthService {
	oauth := &OAuthService{
		db:      db,
		authSvc: authSvc,
		configs: make(map[string]*oauth2.Config),
	}

	if cfg.EnableOAuth {
		// Google OAuth config
		if cfg.OAuthProviders.Google.Enabled {
			oauth.configs["google"] = &oauth2.Config{
				ClientID:     cfg.OAuthProviders.Google.ClientID,
				ClientSecret: cfg.OAuthProviders.Google.ClientSecret,
				RedirectURL:  cfg.OAuthProviders.Google.RedirectURL,
				Scopes:       []string{"openid", "email", "profile"},
				Endpoint:     google.Endpoint,
			}
		}

		// GitHub OAuth config
		if cfg.OAuthProviders.GitHub.Enabled {
			oauth.configs["github"] = &oauth2.Config{
				ClientID:     cfg.OAuthProviders.GitHub.ClientID,
				ClientSecret: cfg.OAuthProviders.GitHub.ClientSecret,
				RedirectURL:  cfg.OAuthProviders.GitHub.RedirectURL,
				Scopes:       []string{"read:user", "user:email"},
				Endpoint:     github.Endpoint,
			}
		}
	}

	return oauth
}

// GetAuthURL returns OAuth authorization URL
func (s *OAuthService) GetAuthURL(provider, state string) (string, error) {
	config, ok := s.configs[provider]
	if !ok {
		return "", fmt.Errorf("unsupported OAuth provider: %s", provider)
	}
	return config.AuthCodeURL(state), nil
}

// HandleCallback handles OAuth callback
func (s *OAuthService) HandleCallback(ctx context.Context, provider, code string) (*TokenResponse, error) {
	config, ok := s.configs[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported OAuth provider: %s", provider)
	}

	// Exchange code for token
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}

	// Get user info based on provider
	var userInfo *OAuthUserInfo
	switch provider {
	case "google":
		userInfo, err = s.getGoogleUserInfo(ctx, token)
	case "github":
		userInfo, err = s.getGitHubUserInfo(ctx, token)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
	if err != nil {
		return nil, err
	}

	// Find or create user
	user, err := s.findOrCreateUser(ctx, provider, userInfo)
	if err != nil {
		return nil, err
	}

	// Generate tokens
	return s.authSvc.generateTokens(*user)
}

// OAuthUserInfo represents user info from OAuth provider
type OAuthUserInfo struct {
	ID        string
	Email     string
	Name      string
	AvatarURL string
}

// getGoogleUserInfo fetches user info from Google
func (s *OAuthService) getGoogleUserInfo(ctx context.Context, token *oauth2.Token) (*OAuthUserInfo, error) {
	client := s.configs["google"].Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: status %d", resp.StatusCode)
	}

	var data struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		Picture   string `json:"picture"`
		Verified  bool   `json:"verified_email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	return &OAuthUserInfo{
		ID:        data.ID,
		Email:     data.Email,
		Name:      data.Name,
		AvatarURL: data.Picture,
	}, nil
}

// getGitHubUserInfo fetches user info from GitHub
func (s *OAuthService) getGitHubUserInfo(ctx context.Context, token *oauth2.Token) (*OAuthUserInfo, error) {
	client := s.configs["github"].Client(ctx, token)
	
	// Get user info
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: status %d", resp.StatusCode)
	}

	var data struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	// If email is empty, fetch from emails endpoint
	email := data.Email
	if email == "" {
		email, err = s.getGitHubPrimaryEmail(ctx, token)
		if err != nil {
			return nil, err
		}
	}

	name := data.Name
	if name == "" {
		name = data.Login
	}

	return &OAuthUserInfo{
		ID:        fmt.Sprintf("%d", data.ID),
		Email:     email,
		Name:      name,
		AvatarURL: data.AvatarURL,
	}, nil
}

// getGitHubPrimaryEmail fetches primary email from GitHub
func (s *OAuthService) getGitHubPrimaryEmail(ctx context.Context, token *oauth2.Token) (string, error) {
	client := s.configs["github"].Client(ctx, token)
	resp, err := client.Get("https://api.github.com/user/emails")
	if err != nil {
		return "", fmt.Errorf("failed to get user emails: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get user emails: status %d", resp.StatusCode)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", fmt.Errorf("failed to decode emails: %w", err)
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}

	if len(emails) > 0 {
		return emails[0].Email, nil
	}

	return "", errors.New("no email found")
}

// findOrCreateUser finds existing user or creates new one
func (s *OAuthService) findOrCreateUser(ctx context.Context, provider string, info *OAuthUserInfo) (*models.User, error) {
	var user models.User
	
	// Try to find by provider ID
	if err := s.db.WithContext(ctx).Where("auth_provider = ? AND provider_id = ?", provider, info.ID).First(&user).Error; err == nil {
		// Update user info
		user.Email = info.Email
		user.Name = info.Name
		user.AvatarURL = info.AvatarURL
		s.db.WithContext(ctx).Save(&user)
		return &user, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Try to find by email
	if err := s.db.WithContext(ctx).Where("email = ?", info.Email).First(&user).Error; err == nil {
		// Link OAuth to existing account
		user.AuthProvider = models.AuthProvider(provider)
		user.ProviderID = info.ID
		user.AvatarURL = info.AvatarURL
		user.EmailVerified = true
		s.db.WithContext(ctx).Save(&user)
		return &user, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Create new user
	user = models.User{
		Email:          info.Email,
		Name:           info.Name,
		AvatarURL:      info.AvatarURL,
		Role:           models.UserRoleUser,
		AuthProvider:   models.AuthProvider(provider),
		ProviderID:     info.ID,
		EmailVerified:  true,
	}

	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &user, nil
}

// IsProviderEnabled checks if OAuth provider is enabled
func (s *OAuthService) IsProviderEnabled(provider string) bool {
	_, ok := s.configs[provider]
	return ok
}

// GetEnabledProviders returns list of enabled OAuth providers
func (s *OAuthService) GetEnabledProviders() []string {
	providers := make([]string, 0, len(s.configs))
	for provider := range s.configs {
		providers = append(providers, provider)
	}
	return providers
}

// RegisterRoutes registers OAuth routes
func (s *OAuthService) RegisterRoutes(r *gin.RouterGroup) {
	oauth := r.Group("/oauth")
	{
		// Get available providers
		oauth.GET("/providers", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"providers": s.GetEnabledProviders(),
			})
		})

		// Initiate OAuth flow
		oauth.GET("/:provider", func(c *gin.Context) {
			provider := c.Param("provider")
			if !s.IsProviderEnabled(provider) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "provider not enabled"})
				return
			}

			state := c.Query("state")
			if state == "" {
				state = uuid.New().String()
			}

			url, err := s.GetAuthURL(provider, state)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.Redirect(http.StatusFound, url)
		})

		// OAuth callback
		oauth.GET("/:provider/callback", func(c *gin.Context) {
			provider := c.Param("provider")
			code := c.Query("code")
			
			if code == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
				return
			}

			tokens, err := s.HandleCallback(c.Request.Context(), provider, code)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, tokens)
		})
	}
}
