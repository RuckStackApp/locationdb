package locationdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

type QueryPlan struct {
	Strategy []string `json:"strategy"`
}

type QueryResponse struct {
	StoreName StoreName    `json:"store_name"`
	Request   QueryRequest `json:"request"`
	Plan      QueryPlan    `json:"plan"`
	Status    string       `json:"status"`
}

type App struct {
	mu      sync.RWMutex
	dataDir string
	catalog Catalog
	handler http.Handler
}

func NewApp(dataDir string) (*App, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("dataDir is required")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	app := &App{
		dataDir: dataDir,
		catalog: Catalog{Stores: map[StoreName]StoreDefinition{}},
	}
	if err := app.loadCatalog(); err != nil {
		return nil, err
	}
	app.handler = app.routes()
	return app, nil
}

func (app *App) Handler() http.Handler {
	return app.handler
}

func (app *App) Catalog() Catalog {
	app.mu.RLock()
	defer app.mu.RUnlock()

	stores := make(map[StoreName]StoreDefinition, len(app.catalog.Stores))
	for name, definition := range app.catalog.Stores {
		stores[name] = definition
	}
	return Catalog{Stores: stores}
}

func (app *App) CreateStore(config StoreConfig) (StoreDefinition, error) {
	if err := config.Validate(); err != nil {
		return StoreDefinition{}, err
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if _, exists := app.catalog.Stores[config.Name]; exists {
		return StoreDefinition{}, fmt.Errorf("store %q already exists", config.Name)
	}
	if err := os.MkdirAll(config.RootPath, 0o755); err != nil {
		return StoreDefinition{}, err
	}
	definition := StoreDefinition{Config: config}
	app.catalog.Stores[config.Name] = definition
	if err := app.saveStoreConfig(definition); err != nil {
		delete(app.catalog.Stores, config.Name)
		return StoreDefinition{}, err
	}
	if err := app.saveCatalogLocked(); err != nil {
		delete(app.catalog.Stores, config.Name)
		return StoreDefinition{}, err
	}
	return definition, nil
}

func (app *App) GetStore(name StoreName) (StoreDefinition, bool) {
	app.mu.RLock()
	defer app.mu.RUnlock()
	definition, ok := app.catalog.Stores[name]
	return definition, ok
}

func (app *App) ExecuteQuery(storeName StoreName, request QueryRequest) (QueryResponse, error) {
	if err := request.Validate(); err != nil {
		return QueryResponse{}, err
	}
	if _, ok := app.GetStore(storeName); !ok {
		return QueryResponse{}, fmt.Errorf("store %q not found", storeName)
	}

	return QueryResponse{
		StoreName: storeName,
		Request:   request,
		Status:    "planned",
		Plan: QueryPlan{Strategy: []string{
			"resolve spatial cells from near filter",
			"collect candidate records from location index",
			"apply label filters",
			"apply valid_at filter",
			"apply exact distance filter",
			"limit final results",
		}},
	}, nil
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

func (app *App) catalogPath() string {
	return filepath.Join(app.dataDir, "catalog.json")
}

func (app *App) loadCatalog() error {
	path := app.catalogPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return err
	}
	if catalog.Stores == nil {
		catalog.Stores = map[StoreName]StoreDefinition{}
	}
	if err := catalog.Validate(); err != nil {
		return err
	}
	app.catalog = catalog
	return nil
}

func (app *App) saveCatalogLocked() error {
	data, err := json.MarshalIndent(app.catalog, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(app.catalogPath(), data, 0o644)
}

func (app *App) saveStoreConfig(definition StoreDefinition) error {
	data, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(definition.Config.RootPath, "store.json")
	return os.WriteFile(path, data, 0o644)
}
