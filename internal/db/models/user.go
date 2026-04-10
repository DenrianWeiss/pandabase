package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserRole represents user role types
type UserRole string

const (
	UserRoleAdmin  UserRole = "admin"
	UserRoleUser   UserRole = "user"
	UserRoleViewer UserRole = "viewer"
)

// AuthProvider represents authentication provider types
type AuthProvider string

const (
	AuthProviderLocal  AuthProvider = "local"
	AuthProviderGoogle AuthProvider = "google"
	AuthProviderGitHub AuthProvider = "github"
)

// User represents a user account
type User struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Email         string         `gorm:"type:varchar(255);not null;uniqueIndex" json:"email"`
	PasswordHash  string         `gorm:"type:varchar(255)" json:"-"` // Empty for OAuth users
	Name          string         `gorm:"type:varchar(255);not null" json:"name"`
	AvatarURL     string         `gorm:"type:text" json:"avatar_url,omitempty"`
	Role          UserRole       `gorm:"type:varchar(50);not null;default:'user'" json:"role"`
	AuthProvider  AuthProvider   `gorm:"type:varchar(50);not null;default:'local'" json:"auth_provider"`
	ProviderID    string         `gorm:"type:varchar(255)" json:"-"` // OAuth provider user ID
	EmailVerified bool           `gorm:"default:false" json:"email_verified"`
	LastLoginAt   *time.Time     `json:"last_login_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`

	// Associations
	Namespaces []Namespace `gorm:"foreignKey:OwnerID" json:"namespaces,omitempty"`
	APITokens  []APIToken  `gorm:"foreignKey:UserID" json:"api_tokens,omitempty"`
}

// TableName specifies the table name for User
func (User) TableName() string {
	return "users"
}

// BeforeCreate hook to set default values
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.Role == "" {
		u.Role = UserRoleUser
	}
	if u.AuthProvider == "" {
		u.AuthProvider = AuthProviderLocal
	}
	return nil
}

// IsAdmin checks if user is admin
func (u *User) IsAdmin() bool {
	return u.Role == UserRoleAdmin
}

// IsOAuthUser checks if user authenticated via OAuth
func (u *User) IsOAuthUser() bool {
	return u.AuthProvider != AuthProviderLocal
}

// NamespaceMember represents a user's membership in a namespace
type NamespaceMember struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	NamespaceID uuid.UUID      `gorm:"type:uuid;not null;index" json:"namespace_id"`
	UserID      uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	Role        string         `gorm:"type:varchar(50);not null;default:'viewer'" json:"role"` // owner, editor, viewer
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	// Associations
	Namespace Namespace `gorm:"foreignKey:NamespaceID" json:"namespace,omitempty"`
	User      User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName specifies the table name for NamespaceMember
func (NamespaceMember) TableName() string {
	return "namespace_members"
}

// APIToken represents a long-lived personal access token for API clients.
type APIToken struct {
	ID         uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	Name       string         `gorm:"type:varchar(255);not null" json:"name"`
	TokenHash  string         `gorm:"type:varchar(64);not null;uniqueIndex" json:"-"`
	Prefix     string         `gorm:"type:varchar(20);not null;index" json:"prefix"`
	LastUsedAt *time.Time     `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	RevokedAt  *time.Time     `json:"revoked_at,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`

	// Associations
	User User `gorm:"foreignKey:UserID" json:"-"`
}

// TableName specifies the table name for APIToken.
func (APIToken) TableName() string {
	return "api_tokens"
}

// Permission constants for namespace access
const (
	NamespaceRoleOwner  = "owner"
	NamespaceRoleEditor = "editor"
	NamespaceRoleViewer = "viewer"
)

// HasPermission checks if member has required permission level
func (m *NamespaceMember) HasPermission(requiredRole string) bool {
	roleHierarchy := map[string]int{
		NamespaceRoleViewer: 1,
		NamespaceRoleEditor: 2,
		NamespaceRoleOwner:  3,
	}

	userLevel := roleHierarchy[m.Role]
	requiredLevel := roleHierarchy[requiredRole]

	return userLevel >= requiredLevel
}
