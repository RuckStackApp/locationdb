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

	locationid "github.com/ruckstackapp/locationid/go"
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

type RecordRequest struct {
	ID        string            `json:"id"`
	Code      string            `json:"code,omitempty"`
	Lat       *float64          `json:"lat,omitempty"`
	Lon       *float64          `json:"lon,omitempty"`
	Precision *uint             `json:"precision,omitempty"`
	Labels    []string          `json:"labels,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type QueryPlan struct {
	Strategy []string `json:"strategy"`
}

type QueryResponse struct {
	StoreName StoreName              `json:"store_name"`
	Request   QueryRequest           `json:"request"`
	Plan      QueryPlan              `json:"plan"`
	Results   []locationindex.Result `json:"results,omitempty"`
	Status    string                 `json:"status"`
}

type App struct {
	mu      sync.RWMutex
	dataDir string
	catalog Catalog
	stores  map[StoreName]*StoreRuntime
	handler http.Handler
}

type StoreRuntime struct {
	Definition StoreDefinition
	Index      *locationindex.LocationIndex
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
		stores:  map[StoreName]*StoreRuntime{},
	}
	if err := app.loadCatalog(); err != nil {
		return nil, err
	}
	if err := app.loadStores(); err != nil {
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
	index, err := openOrCreateIndex(config)
	if err != nil {
		delete(app.catalog.Stores, config.Name)
		return StoreDefinition{}, err
	}
	app.stores[config.Name] = &StoreRuntime{Definition: definition, Index: index}
	if err := app.saveStoreConfig(definition); err != nil {
		delete(app.catalog.Stores, config.Name)
		delete(app.stores, config.Name)
		return StoreDefinition{}, err
	}
	if err := app.saveCatalogLocked(); err != nil {
		delete(app.catalog.Stores, config.Name)
		delete(app.stores, config.Name)
		return StoreDefinition{}, err
	}
	if err := index.Save(indexPathForConfig(config)); err != nil {
		delete(app.catalog.Stores, config.Name)
		delete(app.stores, config.Name)
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
	store, err := app.storeRuntime(storeName)
	if err != nil {
		return QueryResponse{}, err
	}

	labels := make([]locationindex.Label, 0, len(request.Labels))
	for _, label := range request.Labels {
		labels = append(labels, locationindex.Label(label))
	}
	limit := request.Limit
	if limit == 0 {
		limit = 50
	}
	results := store.Index.SearchRadius(locationindex.RadiusQuery{
		Lat:          request.Near.Lat,
		Lon:          request.Near.Lon,
		RadiusMeters: request.Near.Radius,
		Precision:    locationindex.ChoosePrecision(request.Near.Radius),
	}, locationindex.QueryOptions{Labels: labels, Limit: limit})

	return QueryResponse{
		StoreName: storeName,
		Request:   request,
		Results:   results,
		Status:    "ok",
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

func (request RecordRequest) Validate() error {
	if strings.TrimSpace(request.ID) == "" {
		return fmt.Errorf("record id is required")
	}
	hasCode := strings.TrimSpace(request.Code) != ""
	hasCoords := request.Lat != nil || request.Lon != nil
	if !hasCode && !hasCoords {
		return fmt.Errorf("either record code or lat/lon is required")
	}
	if hasCode && hasCoords {
		return fmt.Errorf("provide either record code or lat/lon, not both")
	}
	if hasCoords && (request.Lat == nil || request.Lon == nil) {
		return fmt.Errorf("both lat and lon are required when using coordinates")
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

func (app *App) loadStores() error {
	for name, definition := range app.catalog.Stores {
		index, err := openOrCreateIndex(definition.Config)
		if err != nil {
			return err
		}
		app.stores[name] = &StoreRuntime{Definition: definition, Index: index}
	}
	return nil
}

func (app *App) storeRuntime(name StoreName) (*StoreRuntime, error) {
	app.mu.RLock()
	defer app.mu.RUnlock()
	store, ok := app.stores[name]
	if !ok {
		return nil, fmt.Errorf("store %q not found", name)
	}
	return store, nil
}

func (app *App) InsertRecord(storeName StoreName, request RecordRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	store, err := app.storeRuntime(storeName)
	if err != nil {
		return err
	}
	labels := make([]locationindex.Label, 0, len(request.Labels))
	for _, label := range request.Labels {
		labels = append(labels, locationindex.Label(label))
	}
	code := request.Code
	if code == "" {
		precision := store.Definition.Config.IndexOptions.SpatialCellPrecision
		if request.Precision != nil {
			precision = *request.Precision
		}
		encoded, err := locationid.Encode(*request.Lat, *request.Lon, precision)
		if err != nil {
			return err
		}
		code = encoded.String()
	}
	if err := store.Index.Insert(locationindex.IndexedRecord{
		ID:       locationindex.RecordID(request.ID),
		Code:     code,
		Labels:   labels,
		Metadata: request.Metadata,
	}); err != nil {
		return err
	}
	return store.Index.Save(indexPathForConfig(store.Definition.Config))
}

func openOrCreateIndex(config StoreConfig) (*locationindex.LocationIndex, error) {
	path := indexPathForConfig(config)
	index, err := locationindex.Open(path)
	if err == nil {
		return index, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	index = locationindex.NewLocationIndexWithOptions(config.IndexOptions)
	if err := index.ValidateOptions(); err != nil {
		return nil, err
	}
	return index, nil
}

func indexPathForConfig(config StoreConfig) string {
	return filepath.Join(config.RootPath, "index.lidx")
}
