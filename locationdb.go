package locationdb

import (
	"fmt"
	"strings"
	"time"

	"github.com/ruckstackapp/locationindex"
)

type StoreName string

type StoreConfig struct {
	Name         StoreName                  `json:"name"`
	RootPath     string                     `json:"root_path"`
	IndexOptions locationindex.IndexOptions `json:"index_options"`
	Metadata     map[string]string          `json:"metadata,omitempty"`
}

type StoreDefinition struct {
	Config StoreConfig `json:"config"`
}

type Catalog struct {
	Stores map[StoreName]StoreDefinition `json:"stores"`
}

type NearFilter struct {
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Radius float64 `json:"radius"`
}

type QueryRequest struct {
	Near    *NearFilter `json:"near,omitempty"`
	Labels  []string    `json:"labels,omitempty"`
	ValidAt *time.Time  `json:"valid_at,omitempty"`
	Limit   int         `json:"limit,omitempty"`
}

type QueryLanguageRequest struct {
	Expression string `json:"expression"`
}

func DefaultStoreConfig(name StoreName, rootPath string) StoreConfig {
	return StoreConfig{
		Name:         name,
		RootPath:     rootPath,
		IndexOptions: locationindex.DefaultIndexOptions(),
	}
}

func (config StoreConfig) Validate() error {
	if strings.TrimSpace(string(config.Name)) == "" {
		return fmt.Errorf("store name is required")
	}
	if strings.TrimSpace(config.RootPath) == "" {
		return fmt.Errorf("store root_path is required")
	}
	idx := locationindex.NewLocationIndexWithOptions(config.IndexOptions)
	if err := idx.ValidateOptions(); err != nil {
		return err
	}
	return nil
}

func (catalog Catalog) Validate() error {
	for name, store := range catalog.Stores {
		if name == "" {
			return fmt.Errorf("catalog contains empty store name")
		}
		if err := store.Config.Validate(); err != nil {
			return fmt.Errorf("store %q: %w", name, err)
		}
	}
	return nil
}

func (request QueryRequest) Validate() error {
	if request.Near == nil {
		return fmt.Errorf("near filter is required")
	}
	if request.Near.Radius <= 0 {
		return fmt.Errorf("near.radius must be positive")
	}
	if request.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	return nil
}

func (request QueryLanguageRequest) Validate() error {
	if strings.TrimSpace(request.Expression) == "" {
		return fmt.Errorf("query expression is required")
	}
	return nil
}
