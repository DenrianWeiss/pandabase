package embedder

import (
	"fmt"

	"pandabase/internal/config"
	"pandabase/pkg/plugin"
)

// Factory creates embedders based on configuration
type Factory struct {
	cfg *config.EmbeddingConfig
}

// NewFactory creates a new embedder factory
func NewFactory(cfg *config.EmbeddingConfig) *Factory {
	return &Factory{cfg: cfg}
}

// Create creates an embedder based on configuration
func (f *Factory) Create() (plugin.Embedder, error) {
	// If multimodal is enabled, use Doubao multimodal embedder
	if f.cfg.EnableMultimodal {
		return f.createMultimodalEmbedder()
	}

	// Otherwise use standard OpenAI-compatible embedder
	return f.createStandardEmbedder()
}

// createStandardEmbedder creates a standard OpenAI-compatible embedder
func (f *Factory) createStandardEmbedder() (plugin.Embedder, error) {
	return NewOpenAIEmbedder(
		f.cfg.APIURL,
		f.cfg.APIKey,
		f.cfg.Model,
		f.cfg.Dimensions,
		false, // multimodal disabled
	), nil
}

// createMultimodalEmbedder creates a multimodal embedder
func (f *Factory) createMultimodalEmbedder() (plugin.Embedder, error) {
	// For now, we only support Doubao-style multimodal
	// In the future, this could be extended to support other providers
	return NewDoubaoMultimodalEmbedder(
		f.cfg.APIURL,
		f.cfg.APIKey,
		f.cfg.Model,
		f.cfg.Dimensions,
	), nil
}

// CreateMultimodal creates a multimodal embedder if configured
func (f *Factory) CreateMultimodal() (*DoubaoMultimodalEmbedder, error) {
	if !f.cfg.EnableMultimodal {
		return nil, fmt.Errorf("multimodal embeddings not enabled in configuration")
	}

	embedder := NewDoubaoMultimodalEmbedder(
		f.cfg.APIURL,
		f.cfg.APIKey,
		f.cfg.Model,
		f.cfg.Dimensions,
	)

	return embedder, nil
}
