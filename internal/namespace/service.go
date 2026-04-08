package namespace

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"pandabase/internal/db/models"
)

// Service provides namespace management operations
type Service struct {
	db *gorm.DB
}

// NewService creates a new namespace service
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// CreateNamespace creates a new namespace and assigns the creator as owner
func (s *Service) CreateNamespace(ctx context.Context, name, description string, ownerID uuid.UUID) (*models.Namespace, error) {
	namespace := &models.Namespace{
		Name:        name,
		Description: description,
		OwnerID:     ownerID,
		AccessLevel: "private",
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(namespace).Error; err != nil {
			return err
		}

		member := &models.NamespaceMember{
			NamespaceID: namespace.ID,
			UserID:      ownerID,
			Role:        models.NamespaceRoleOwner,
		}
		if err := tx.Create(member).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return namespace, nil
}

// GetNamespace retrieves a namespace if the user has access
func (s *Service) GetNamespace(ctx context.Context, namespaceID, userID uuid.UUID) (*models.Namespace, error) {
	var namespace models.Namespace
	err := s.db.WithContext(ctx).First(&namespace, "id = ?", namespaceID).Error
	if err != nil {
		return nil, err
	}

	if err := s.checkAccess(ctx, namespaceID, userID, models.NamespaceRoleViewer); err != nil {
		return nil, err
	}

	return &namespace, nil
}

// ListNamespaces lists all namespaces a user has access to
func (s *Service) ListNamespaces(ctx context.Context, userID uuid.UUID) ([]models.Namespace, error) {
	var namespaces []models.Namespace
	err := s.db.WithContext(ctx).
		Joins("JOIN namespace_members ON namespace_members.namespace_id = namespaces.id").
		Where("namespace_members.user_id = ? AND namespaces.deleted_at IS NULL", userID).
		Find(&namespaces).Error

	return namespaces, err
}

// UpdateNamespace updates namespace details (requires owner/editor role)
func (s *Service) UpdateNamespace(ctx context.Context, namespaceID, userID uuid.UUID, name, description string) (*models.Namespace, error) {
	if err := s.checkAccess(ctx, namespaceID, userID, models.NamespaceRoleEditor); err != nil {
		return nil, err
	}

	var namespace models.Namespace
	if err := s.db.WithContext(ctx).First(&namespace, "id = ?", namespaceID).Error; err != nil {
		return nil, err
	}

	namespace.Name = name
	namespace.Description = description

	if err := s.db.WithContext(ctx).Save(&namespace).Error; err != nil {
		return nil, err
	}

	return &namespace, nil
}

// DeleteNamespace deletes a namespace (requires owner role)
func (s *Service) DeleteNamespace(ctx context.Context, namespaceID, userID uuid.UUID) error {
	if err := s.checkAccess(ctx, namespaceID, userID, models.NamespaceRoleOwner); err != nil {
		return err
	}

	// Will cascade delete documents based on foreign keys
	return s.db.WithContext(ctx).Delete(&models.Namespace{}, "id = ?", namespaceID).Error
}

// checkAccess verifies if a user has the required role in a namespace
func (s *Service) checkAccess(ctx context.Context, namespaceID, userID uuid.UUID, requiredRole string) error {
	var member models.NamespaceMember
	err := s.db.WithContext(ctx).Where("namespace_id = ? AND user_id = ?", namespaceID, userID).First(&member).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("access denied")
		}
		return err
	}

	if !member.HasPermission(requiredRole) {
		return errors.New("insufficient permissions")
	}

	return nil
}
