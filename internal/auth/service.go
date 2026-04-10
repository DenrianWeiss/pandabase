package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"pandabase/internal/db/models"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidToken       = errors.New("invalid token")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidPassword    = errors.New("invalid password")
)

// Config holds authentication configuration
type Config struct {
	JWTSecret          string        `mapstructure:"jwt_secret"`
	JWTExpiry          time.Duration `mapstructure:"jwt_expiry"`
	RefreshTokenExpiry time.Duration `mapstructure:"refresh_token_expiry"`
	EnableOAuth        bool          `mapstructure:"enable_oauth"`
	OAuthProviders     OAuthConfig   `mapstructure:"oauth_providers"`
}

// OAuthConfig holds OAuth provider configurations
type OAuthConfig struct {
	Google OAuthProviderConfig `mapstructure:"google"`
	GitHub OAuthProviderConfig `mapstructure:"github"`
}

// OAuthProviderConfig holds a single OAuth provider configuration
type OAuthProviderConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url"`
}

// DefaultConfig returns default auth configuration
func DefaultConfig() Config {
	return Config{
		JWTSecret:          GenerateRandomSecret(),
		JWTExpiry:          24 * time.Hour,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		EnableOAuth:        false,
	}
}

func GenerateRandomSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func generatePlainAPIToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "pdb_" + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Service handles authentication logic
type Service struct {
	db     *gorm.DB
	config Config
}

// NewService creates a new auth service
func NewService(db *gorm.DB, config Config) *Service {
	return &Service{
		db:     db,
		config: config,
	}
}

// RegisterRequest represents user registration request
type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Name     string `json:"name" binding:"required"`
}

// LoginRequest represents user login request
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// TokenResponse represents authentication response
type TokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int64        `json:"expires_in"`
	User         UserResponse `json:"user"`
}

// UserResponse represents user data in responses
type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	Role      string    `json:"role"`
}

// APITokenResponse represents API token metadata returned to clients.
type APITokenResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreateAPITokenRequest represents create-token payload.
type CreateAPITokenRequest struct {
	Name          string `json:"name" binding:"required"`
	ExpiresInDays int    `json:"expires_in_days,omitempty"`
}

// CreatedAPITokenResponse includes one-time plain token value.
type CreatedAPITokenResponse struct {
	Token     APITokenResponse `json:"token"`
	PlainText string           `json:"plain_text"`
}

// ChangePasswordRequest represents self-service password update payload.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=8"`
}

// Claims represents JWT claims
type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	Role   string    `json:"role"`
	jwt.RegisteredClaims
}

// Register creates the first administrator account (only allowed if no users exist)
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*TokenResponse, error) {
	// Check if already initialized
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	if count > 0 {
		return nil, errors.New("initial registration closed: system already initialized")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create first user as admin
	user := models.User{
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		Name:         req.Name,
		Role:         models.UserRoleAdmin,
		AuthProvider: models.AuthProviderLocal,
	}

	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Generate tokens
	return s.generateTokens(user)
}

// CreateUser creates a new user account (used by admins)
func (s *Service) CreateUser(ctx context.Context, req RegisterRequest) (*models.User, error) {
	// Check if user exists
	var existing models.User
	if err := s.db.WithContext(ctx).Where("email = ?", req.Email).First(&existing).Error; err == nil {
		return nil, ErrUserExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("database error: %w", err)
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user
	user := models.User{
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		Name:         req.Name,
		Role:         models.UserRoleUser,
		AuthProvider: models.AuthProviderLocal,
	}

	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &user, nil
}

// Login authenticates a user
func (s *Service) Login(ctx context.Context, req LoginRequest) (*TokenResponse, error) {
	var user models.User
	if err := s.db.WithContext(ctx).Where("email = ? AND auth_provider = ?", req.Email, models.AuthProviderLocal).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	// Update last login
	s.db.WithContext(ctx).Model(&user).Update("last_login_at", time.Now())

	// Generate tokens
	return s.generateTokens(user)
}

// ChangePassword updates user's local password after verifying current password.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, req ChangePasswordRequest) error {
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}

	if user.AuthProvider != models.AuthProviderLocal {
		return errors.New("oauth users cannot reset local password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return ErrInvalidPassword
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	return s.db.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", userID).
		Update("password_hash", string(newHash)).Error
}

// CreateAPIToken creates a persistent personal access token for API clients.
func (s *Service) CreateAPIToken(ctx context.Context, userID uuid.UUID, req CreateAPITokenRequest) (*CreatedAPITokenResponse, error) {
	plain, err := generatePlainAPIToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	prefix := plain
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}

	record := models.APIToken{
		UserID:    userID,
		Name:      req.Name,
		TokenHash: hashToken(plain),
		Prefix:    prefix,
		ExpiresAt: expiresAt,
	}

	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, err
	}

	return &CreatedAPITokenResponse{
		Token: APITokenResponse{
			ID:         record.ID,
			Name:       record.Name,
			Prefix:     record.Prefix,
			LastUsedAt: record.LastUsedAt,
			ExpiresAt:  record.ExpiresAt,
			CreatedAt:  record.CreatedAt,
		},
		PlainText: plain,
	}, nil
}

// ListAPITokens lists active API tokens created by the user.
func (s *Service) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]APITokenResponse, error) {
	var records []models.APIToken
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, err
	}

	out := make([]APITokenResponse, 0, len(records))
	for _, r := range records {
		out = append(out, APITokenResponse{
			ID:         r.ID,
			Name:       r.Name,
			Prefix:     r.Prefix,
			LastUsedAt: r.LastUsedAt,
			ExpiresAt:  r.ExpiresAt,
			CreatedAt:  r.CreatedAt,
		})
	}
	return out, nil
}

// DeleteAPIToken revokes a token that belongs to the current user.
func (s *Service) DeleteAPIToken(ctx context.Context, userID, tokenID uuid.UUID) error {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&models.APIToken{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", tokenID, userID).
		Update("revoked_at", &now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *Service) tryAuthenticateAPIToken(ctx context.Context, tokenString string) (*models.User, error) {
	var record models.APIToken
	now := time.Now()
	err := s.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", hashToken(tokenString), now).
		First(&record).Error
	if err != nil {
		return nil, err
	}

	_ = s.db.WithContext(ctx).Model(&models.APIToken{}).Where("id = ?", record.ID).Update("last_used_at", now).Error

	var user models.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", record.UserID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByID retrieves a user by ID
func (s *Service) GetUserByID(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// RefreshToken generates new access token from refresh token
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	// Parse refresh token
	token, err := jwt.Parse(refreshToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidToken
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		// Refresh token uses RegisteredClaims which stores user ID in "sub"
		userIDStr, ok = claims["sub"].(string)
	}
	if !ok {
		return nil, ErrInvalidToken
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Get user
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Generate new tokens
	return s.generateTokens(*user)
}

// generateTokens creates access and refresh tokens
func (s *Service) generateTokens(user models.User) (*TokenResponse, error) {
	// Access token claims
	accessClaims := Claims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   string(user.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.config.JWTExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   user.ID.String(),
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString([]byte(s.config.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Refresh token claims
	refreshClaims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.config.RefreshTokenExpiry)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Subject:   user.ID.String(),
		ID:        uuid.New().String(),
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(s.config.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenResponse{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresIn:    int64(s.config.JWTExpiry.Seconds()),
		User: UserResponse{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			AvatarURL: user.AvatarURL,
			Role:      string(user.Role),
		},
	}, nil
}

// Middleware creates Gin middleware for JWT authentication
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		// Extract Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			return
		}

		tokenString := parts[1]

		// Parse and validate JWT first.
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(s.config.JWTSecret), nil
		})
		if err == nil {
			claims, ok := token.Claims.(*Claims)
			if ok && token.Valid {
				// Set user info in context
				c.Set("userID", claims.UserID)
				c.Set("userEmail", claims.Email)
				c.Set("userRole", claims.Role)
				c.Next()
				return
			}
		}

		// Fallback to persistent API token.
		user, apiTokenErr := s.tryAuthenticateAPIToken(c.Request.Context(), tokenString)
		if apiTokenErr == nil {
			c.Set("userID", user.ID)
			c.Set("userEmail", user.Email)
			c.Set("userRole", string(user.Role))
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
	}
}

// GetUserIDFromContext extracts user ID from Gin context
func GetUserIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	userID, exists := c.Get("userID")
	if !exists {
		return uuid.Nil, false
	}
	id, ok := userID.(uuid.UUID)
	return id, ok
}

// GetUserRoleFromContext extracts user role from Gin context
func GetUserRoleFromContext(c *gin.Context) (string, bool) {
	role, exists := c.Get("userRole")
	if !exists {
		return "", false
	}
	r, ok := role.(string)
	return r, ok
}

// AdminOnly middleware restricts access to admins only
func (s *Service) AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := GetUserRoleFromContext(c)
		if !ok || role != string(models.UserRoleAdmin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			return
		}
		c.Next()
	}
}

// ListUsers retrieves all users
func (s *Service) ListUsers(ctx context.Context) ([]models.User, error) {
	var users []models.User
	if err := s.db.WithContext(ctx).Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// UpdateUser updates an existing user
func (s *Service) UpdateUser(ctx context.Context, userID uuid.UUID, updates map[string]interface{}) error {
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return err
	}
	return nil
}

// DeleteUser deletes a user
func (s *Service) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	if err := s.db.WithContext(ctx).Delete(&models.User{}, "id = ?", userID).Error; err != nil {
		return err
	}
	return nil
}

// IsInitialized checks if there are any users in the system
func (s *Service) IsInitialized(ctx context.Context) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// OptionalAuth middleware allows both authenticated and unauthenticated requests
func (s *Service) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Set("userID", uuid.Nil)
			c.Next()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.Set("userID", uuid.Nil)
			c.Next()
			return
		}

		tokenString := parts[1]
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(s.config.JWTSecret), nil
		})
		if err != nil {
			c.Set("userID", uuid.Nil)
			c.Next()
			return
		}

		claims, ok := token.Claims.(*Claims)
		if !ok || !token.Valid {
			c.Set("userID", uuid.Nil)
			c.Next()
			return
		}

		c.Set("userID", claims.UserID)
		c.Set("userEmail", claims.Email)
		c.Set("userRole", claims.Role)

		c.Next()
	}
}
