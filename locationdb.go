package locationdb

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	locationid "github.com/ruckstackapp/locationid/go"
	"github.com/ruckstackapp/locationindex"
)

var recordStoreMagic = [4]byte{'L', 'D', 'B', 'R'}

const recordStoreVersion = 1

type StoreName string

type StoreConfig struct {
	Name         StoreName                  `json:"name"`
	RootPath     string                     `json:"root_path"`
	IndexOptions locationindex.IndexOptions `json:"index_options"`
	Schema       *RecordSchema              `json:"schema,omitempty"`
	Metadata     map[string]string          `json:"metadata,omitempty"`
}

type StoreDefinition struct {
	Config StoreConfig `json:"config"`
}

type Catalog struct {
	Stores map[StoreName]StoreDefinition `json:"stores"`
}

type NearFilter struct {
	Field  string  `json:"field,omitempty"`
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
	ID         string            `json:"id"`
	Code       string            `json:"code,omitempty"`
	Lat        *float64          `json:"lat,omitempty"`
	Lon        *float64          `json:"lon,omitempty"`
	Precision  *uint             `json:"precision,omitempty"`
	ValidFrom  *time.Time        `json:"valid_from,omitempty"`
	ValidUntil *time.Time        `json:"valid_until,omitempty"`
	Fields     map[string]any    `json:"fields,omitempty"`
	Labels     []string          `json:"labels,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type FieldType string

type CollectionType string

const (
	CollectionTypePointOfInterest CollectionType = "point_of_interest"
	CollectionTypeMovingObject    CollectionType = "moving_object"

	FieldTypeGeometry FieldType = "geometry"
	FieldTypeString   FieldType = "string"
	FieldTypeInt      FieldType = "int"
	FieldTypeFloat    FieldType = "float"
	FieldTypeBool     FieldType = "bool"
	FieldTypeDateTime FieldType = "datetime"
)

type GeometryType string

const GeometryTypePoint GeometryType = "point"

type FieldSchema struct {
	Type         FieldType    `json:"type"`
	GeometryType GeometryType `json:"geometry_type,omitempty"`
	Required     bool         `json:"required,omitempty"`
	Indexed      bool         `json:"indexed,omitempty"`
	Enum         []string     `json:"enum,omitempty"`
}

type RecordSchema struct {
	CollectionType CollectionType         `json:"collection_type,omitempty"`
	Fields         map[string]FieldSchema `json:"fields"`
}

type StoredValue struct {
	Type         FieldType    `json:"type"`
	GeometryType GeometryType `json:"geometry_type,omitempty"`
	String       *string      `json:"string,omitempty"`
	Int          *int64       `json:"int,omitempty"`
	Float        *float64     `json:"float,omitempty"`
	Bool         *bool        `json:"bool,omitempty"`
	DateTime     *time.Time   `json:"datetime,omitempty"`
	Point        *StoredPoint `json:"point,omitempty"`
}

type StoredPoint struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Code string  `json:"code,omitempty"`
}

type StoredRecord struct {
	ID         string                 `json:"id"`
	Code       string                 `json:"code"`
	ValidFrom  *time.Time             `json:"valid_from,omitempty"`
	ValidUntil *time.Time             `json:"valid_until,omitempty"`
	Fields     map[string]StoredValue `json:"fields,omitempty"`
	Labels     []string               `json:"labels,omitempty"`
	Metadata   map[string]string      `json:"metadata,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
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
	Records    map[string]StoredRecord
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
	records, err := loadOrCreateRecords(config)
	if err != nil {
		delete(app.catalog.Stores, config.Name)
		return StoreDefinition{}, err
	}
	index, err := openOrCreateIndex(config, records)
	if err != nil {
		delete(app.catalog.Stores, config.Name)
		return StoreDefinition{}, err
	}
	app.stores[config.Name] = &StoreRuntime{Definition: definition, Index: index, Records: records}
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
	if err := saveRecords(config, records); err != nil {
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

func (app *App) UpdateStoreSchema(name StoreName, schema *RecordSchema) (StoreDefinition, error) {
	if schema != nil {
		if err := schema.Validate(); err != nil {
			return StoreDefinition{}, err
		}
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	definition, ok := app.catalog.Stores[name]
	if !ok {
		return StoreDefinition{}, fmt.Errorf("store %q not found", name)
	}
	definition.Config.Schema = schema
	app.catalog.Stores[name] = definition
	store, ok := app.stores[name]
	if ok {
		store.Definition = definition
	}
	if err := app.saveStoreConfig(definition); err != nil {
		return StoreDefinition{}, err
	}
	if err := app.saveCatalogLocked(); err != nil {
		return StoreDefinition{}, err
	}
	return definition, nil
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
	indexLimit := limit
	if request.ValidAt != nil {
		indexLimit = 0
	}
	results := store.Index.SearchRadius(locationindex.RadiusQuery{
		Lat:          request.Near.Lat,
		Lon:          request.Near.Lon,
		RadiusMeters: request.Near.Radius,
		Precision:    locationindex.ChoosePrecision(request.Near.Radius),
	}, locationindex.QueryOptions{Labels: labels, Limit: indexLimit})
	results = filterResultsByValidity(results, store.Records, request.ValidAt, limit)

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
	if config.Schema != nil {
		normalizeRecordSchema(config.Schema)
		if err := config.Schema.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (schema *RecordSchema) Validate() error {
	if schema == nil {
		return nil
	}
	normalizeRecordSchema(schema)
	switch schema.CollectionType {
	case CollectionTypePointOfInterest, CollectionTypeMovingObject:
	default:
		return fmt.Errorf("unsupported collection type %q", schema.CollectionType)
	}
	for name, field := range schema.Fields {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("schema field name is required")
		}
		switch field.Type {
		case FieldTypeString, FieldTypeInt, FieldTypeFloat, FieldTypeBool, FieldTypeDateTime:
			if field.GeometryType != "" {
				return fmt.Errorf("geometry_type is only valid for geometry fields: %q", name)
			}
		case FieldTypeGeometry:
			if field.GeometryType == "" {
				field.GeometryType = GeometryTypePoint
				schema.Fields[name] = field
			}
			if field.GeometryType != GeometryTypePoint {
				return fmt.Errorf("unsupported geometry type %q for field %q", field.GeometryType, name)
			}
			if !field.Indexed {
				field.Indexed = true
				schema.Fields[name] = field
			}
		default:
			return fmt.Errorf("unsupported field type %q for field %q", field.Type, name)
		}
		if len(field.Enum) > 0 && field.Type != FieldTypeString {
			return fmt.Errorf("enum is only supported for string fields: %q", name)
		}
	}
	return nil
}

func normalizeRecordSchema(schema *RecordSchema) {
	if schema == nil {
		return
	}
	if schema.CollectionType == "" {
		schema.CollectionType = CollectionTypePointOfInterest
	}
	if schema.Fields == nil {
		schema.Fields = map[string]FieldSchema{}
	}
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
	if hasCode && hasCoords {
		return fmt.Errorf("provide either record code or lat/lon, not both")
	}
	if hasCoords && (request.Lat == nil || request.Lon == nil) {
		return fmt.Errorf("both lat and lon are required when using coordinates")
	}
	if request.ValidFrom != nil && request.ValidUntil != nil && request.ValidFrom.After(*request.ValidUntil) {
		return fmt.Errorf("valid_from must be before or equal to valid_until")
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
		records, err := loadOrCreateRecords(definition.Config)
		if err != nil {
			return err
		}
		index, err := openOrCreateIndex(definition.Config, records)
		if err != nil {
			return err
		}
		app.stores[name] = &StoreRuntime{Definition: definition, Index: index, Records: records}
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
	fields, err := coerceRecordFields(store.Definition.Config.Schema, request.Fields)
	if err != nil {
		return err
	}
	labels := make([]locationindex.Label, 0, len(request.Labels))
	for _, label := range request.Labels {
		labels = append(labels, locationindex.Label(label))
	}
	code := request.Code
	if code == "" {
		if request.Lat != nil && request.Lon != nil {
			precision := store.Definition.Config.IndexOptions.SpatialCellPrecision
			if request.Precision != nil {
				precision = *request.Precision
			}
			encoded, err := locationid.Encode(*request.Lat, *request.Lon, precision)
			if err != nil {
				return err
			}
			code = encoded.String()
		} else {
			derivedCode, err := deriveIndexedCodeFromFields(store.Definition.Config.Schema, store.Definition.Config.IndexOptions.SpatialCellPrecision, fields)
			if err != nil {
				return err
			}
			code = derivedCode
		}
	}
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("record must provide an indexable location")
	}
	stored := StoredRecord{
		ID:         request.ID,
		Code:       code,
		ValidFrom:  cloneTimePtr(request.ValidFrom),
		ValidUntil: cloneTimePtr(request.ValidUntil),
		Fields:     fields,
		Labels:     append([]string(nil), request.Labels...),
		Metadata:   cloneStringMap(request.Metadata),
		CreatedAt:  time.Now().UTC(),
	}
	store.Records[stored.ID] = stored
	if err := saveRecords(store.Definition.Config, store.Records); err != nil {
		delete(store.Records, stored.ID)
		return err
	}
	if err := store.Index.Insert(locationindex.IndexedRecord{
		ID:       locationindex.RecordID(request.ID),
		Code:     code,
		Labels:   labels,
		Metadata: request.Metadata,
	}); err != nil {
		delete(store.Records, stored.ID)
		_ = saveRecords(store.Definition.Config, store.Records)
		return err
	}
	return store.Index.Save(indexPathForConfig(store.Definition.Config))
}

func openOrCreateIndex(config StoreConfig, records map[string]StoredRecord) (*locationindex.LocationIndex, error) {
	path := indexPathForConfig(config)
	index, err := locationindex.Open(path)
	if err == nil {
		return index, nil
	}
	if !os.IsNotExist(err) {
		index, rebuildErr := rebuildIndex(config, records)
		if rebuildErr != nil {
			return nil, err
		}
		return index, nil
	}
	return rebuildIndex(config, records)
}

func indexPathForConfig(config StoreConfig) string {
	return filepath.Join(config.RootPath, "index.lidx")
}

func recordsPathForConfig(config StoreConfig) string {
	return filepath.Join(config.RootPath, "records.ldb")
}

func loadOrCreateRecords(config StoreConfig) (map[string]StoredRecord, error) {
	path := recordsPathForConfig(config)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]StoredRecord{}, nil
		}
		return nil, err
	}
	defer file.Close()

	records, err := loadBinaryRecords(file)
	if err != nil {
		return nil, err
	}
	return records, nil
}

func saveRecords(config StoreConfig, records map[string]StoredRecord) error {
	path := recordsPathForConfig(config)
	ordered := sortedStoredRecords(records)
	return writeAtomically(path, func(file *os.File) error {
		if err := saveBinaryRecords(file, ordered); err != nil {
			return err
		}
		return file.Sync()
	})
}

func rebuildIndex(config StoreConfig, records map[string]StoredRecord) (*locationindex.LocationIndex, error) {
	index := locationindex.NewLocationIndexWithOptions(config.IndexOptions)
	if err := index.ValidateOptions(); err != nil {
		return nil, err
	}
	for _, record := range records {
		labels := make([]locationindex.Label, 0, len(record.Labels))
		for _, label := range record.Labels {
			labels = append(labels, locationindex.Label(label))
		}
		if err := index.Insert(locationindex.IndexedRecord{
			ID:       locationindex.RecordID(record.ID),
			Code:     record.Code,
			Labels:   labels,
			Metadata: cloneStringMap(record.Metadata),
		}); err != nil {
			return nil, err
		}
	}
	return index, nil
}

func filterResultsByValidity(results []locationindex.Result, records map[string]StoredRecord, validAt *time.Time, limit int) []locationindex.Result {
	if validAt == nil {
		return results
	}
	filtered := make([]locationindex.Result, 0, len(results))
	for _, result := range results {
		record, ok := records[string(result.Record.ID)]
		if !ok || !recordValidAt(record, *validAt) {
			continue
		}
		filtered = append(filtered, result)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered
}

func recordValidAt(record StoredRecord, validAt time.Time) bool {
	if record.ValidFrom != nil && validAt.Before(*record.ValidFrom) {
		return false
	}
	if record.ValidUntil != nil && validAt.After(*record.ValidUntil) {
		return false
	}
	return true
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func coerceRecordFields(schema *RecordSchema, input map[string]any) (map[string]StoredValue, error) {
	if schema == nil {
		return nil, nil
	}
	if input == nil {
		input = map[string]any{}
	}
	out := make(map[string]StoredValue, len(input))
	for name, field := range schema.Fields {
		value, ok := input[name]
		if !ok {
			if field.Required {
				return nil, fmt.Errorf("missing required field %q", name)
			}
			continue
		}
		stored, err := coerceStoredValue(name, field, value)
		if err != nil {
			return nil, err
		}
		out[name] = stored
	}
	for name := range input {
		if _, ok := schema.Fields[name]; !ok {
			return nil, fmt.Errorf("field %q is not defined in schema", name)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func coerceStoredValue(name string, field FieldSchema, value any) (StoredValue, error) {
	switch field.Type {
	case FieldTypeGeometry:
		if field.GeometryType != GeometryTypePoint {
			return StoredValue{}, fmt.Errorf("unsupported geometry type %q for field %q", field.GeometryType, name)
		}
		point, err := coercePointValue(name, value)
		if err != nil {
			return StoredValue{}, err
		}
		return StoredValue{Type: field.Type, GeometryType: field.GeometryType, Point: point}, nil
	case FieldTypeString:
		stringValue, ok := value.(string)
		if !ok {
			return StoredValue{}, fmt.Errorf("field %q must be a string", name)
		}
		if len(field.Enum) > 0 {
			matched := false
			for _, allowed := range field.Enum {
				if stringValue == allowed {
					matched = true
					break
				}
			}
			if !matched {
				return StoredValue{}, fmt.Errorf("field %q must be one of the configured enum values", name)
			}
		}
		return StoredValue{Type: field.Type, String: &stringValue}, nil
	case FieldTypeInt:
		number, ok := value.(float64)
		if !ok || number != float64(int64(number)) {
			return StoredValue{}, fmt.Errorf("field %q must be an integer", name)
		}
		intValue := int64(number)
		return StoredValue{Type: field.Type, Int: &intValue}, nil
	case FieldTypeFloat:
		number, ok := value.(float64)
		if !ok {
			return StoredValue{}, fmt.Errorf("field %q must be a number", name)
		}
		return StoredValue{Type: field.Type, Float: &number}, nil
	case FieldTypeBool:
		boolValue, ok := value.(bool)
		if !ok {
			return StoredValue{}, fmt.Errorf("field %q must be a boolean", name)
		}
		return StoredValue{Type: field.Type, Bool: &boolValue}, nil
	case FieldTypeDateTime:
		stringValue, ok := value.(string)
		if !ok {
			return StoredValue{}, fmt.Errorf("field %q must be an RFC3339 datetime string", name)
		}
		timeValue, err := time.Parse(time.RFC3339, stringValue)
		if err != nil {
			return StoredValue{}, fmt.Errorf("field %q must be an RFC3339 datetime string", name)
		}
		timeValue = timeValue.UTC()
		return StoredValue{Type: field.Type, DateTime: &timeValue}, nil
	default:
		return StoredValue{}, fmt.Errorf("unsupported field type %q", field.Type)
	}
}

func coercePointValue(name string, value any) (*StoredPoint, error) {
	mapValue, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("field %q must be an object for geometry point values", name)
	}
	code, hasCode := stringFromAny(mapValue["code"])
	lat, hasLat := floatFromAny(mapValue["lat"])
	lon, hasLon := floatFromAny(mapValue["lon"])
	if hasCode && (hasLat || hasLon) {
		return nil, fmt.Errorf("field %q must provide either code or lat/lon", name)
	}
	if hasCode {
		decoded, err := locationid.Decode(locationid.New(code))
		if err != nil {
			return nil, err
		}
		return &StoredPoint{Lat: decoded.CenterLat, Lon: decoded.CenterLon, Code: code}, nil
	}
	if !hasLat || !hasLon {
		return nil, fmt.Errorf("field %q must provide code or both lat and lon", name)
	}
	return &StoredPoint{Lat: lat, Lon: lon}, nil
}

func deriveIndexedCodeFromFields(schema *RecordSchema, precision uint, fields map[string]StoredValue) (string, error) {
	if schema == nil {
		return "", nil
	}
	for name, field := range schema.Fields {
		if field.Type != FieldTypeGeometry || !field.Indexed {
			continue
		}
		stored, ok := fields[name]
		if !ok || stored.Point == nil {
			continue
		}
		if stored.Point.Code != "" {
			return stored.Point.Code, nil
		}
		encoded, err := locationid.Encode(stored.Point.Lat, stored.Point.Lon, precision)
		if err != nil {
			return "", err
		}
		point := *stored.Point
		point.Code = encoded.String()
		stored.Point = &point
		fields[name] = stored
		return point.Code, nil
	}
	return "", nil
}

func stringFromAny(value any) (string, bool) {
	stringValue, ok := value.(string)
	return stringValue, ok && strings.TrimSpace(stringValue) != ""
}

func floatFromAny(value any) (float64, bool) {
	floatValue, ok := value.(float64)
	return floatValue, ok
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	timeCopy := value.UTC()
	return &timeCopy
}

func saveBinaryRecords(writer io.Writer, records []StoredRecord) error {
	buffered := bufio.NewWriter(writer)
	if _, err := buffered.Write(recordStoreMagic[:]); err != nil {
		return err
	}
	if err := binary.Write(buffered, binary.BigEndian, uint32(recordStoreVersion)); err != nil {
		return err
	}
	if err := binary.Write(buffered, binary.BigEndian, uint32(len(records))); err != nil {
		return err
	}
	for _, record := range records {
		if err := writeStoredRecord(buffered, record); err != nil {
			return err
		}
	}
	return buffered.Flush()
}

func loadBinaryRecords(reader io.Reader) (map[string]StoredRecord, error) {
	buffered := bufio.NewReader(reader)
	magic := [4]byte{}
	if _, err := io.ReadFull(buffered, magic[:]); err != nil {
		return nil, err
	}
	if magic != recordStoreMagic {
		return nil, fmt.Errorf("unsupported record store format")
	}
	var version uint32
	if err := binary.Read(buffered, binary.BigEndian, &version); err != nil {
		return nil, err
	}
	if version != recordStoreVersion {
		return nil, fmt.Errorf("unsupported record store version")
	}
	var count uint32
	if err := binary.Read(buffered, binary.BigEndian, &count); err != nil {
		return nil, err
	}
	records := make(map[string]StoredRecord, count)
	for i := uint32(0); i < count; i++ {
		record, err := readStoredRecord(buffered)
		if err != nil {
			return nil, err
		}
		records[record.ID] = record
	}
	return records, nil
}

func writeStoredRecord(writer io.Writer, record StoredRecord) error {
	if err := writeString(writer, record.ID); err != nil {
		return err
	}
	if err := writeString(writer, record.Code); err != nil {
		return err
	}
	if err := writeOptionalTime(writer, record.ValidFrom); err != nil {
		return err
	}
	if err := writeOptionalTime(writer, record.ValidUntil); err != nil {
		return err
	}
	if err := binary.Write(writer, binary.BigEndian, uint32(len(record.Fields))); err != nil {
		return err
	}
	for _, key := range sortedStoredValueKeys(record.Fields) {
		if err := writeString(writer, key); err != nil {
			return err
		}
		if err := writeStoredValue(writer, record.Fields[key]); err != nil {
			return err
		}
	}
	if err := binary.Write(writer, binary.BigEndian, int64(record.CreatedAt.UTC().UnixNano())); err != nil {
		return err
	}
	if err := binary.Write(writer, binary.BigEndian, uint32(len(record.Labels))); err != nil {
		return err
	}
	for _, label := range record.Labels {
		if err := writeString(writer, label); err != nil {
			return err
		}
	}
	if err := binary.Write(writer, binary.BigEndian, uint32(len(record.Metadata))); err != nil {
		return err
	}
	for _, key := range sortedMetadataKeys(record.Metadata) {
		if err := writeString(writer, key); err != nil {
			return err
		}
		if err := writeString(writer, record.Metadata[key]); err != nil {
			return err
		}
	}
	return nil
}

func readStoredRecord(reader io.Reader) (StoredRecord, error) {
	id, err := readString(reader)
	if err != nil {
		return StoredRecord{}, err
	}
	code, err := readString(reader)
	if err != nil {
		return StoredRecord{}, err
	}
	validFrom, err := readOptionalTime(reader)
	if err != nil {
		return StoredRecord{}, err
	}
	validUntil, err := readOptionalTime(reader)
	if err != nil {
		return StoredRecord{}, err
	}
	var fieldCount uint32
	if err := binary.Read(reader, binary.BigEndian, &fieldCount); err != nil {
		return StoredRecord{}, err
	}
	fields := make(map[string]StoredValue, fieldCount)
	for i := uint32(0); i < fieldCount; i++ {
		key, err := readString(reader)
		if err != nil {
			return StoredRecord{}, err
		}
		value, err := readStoredValue(reader)
		if err != nil {
			return StoredRecord{}, err
		}
		fields[key] = value
	}
	var createdAtNano int64
	if err := binary.Read(reader, binary.BigEndian, &createdAtNano); err != nil {
		return StoredRecord{}, err
	}
	var labelCount uint32
	if err := binary.Read(reader, binary.BigEndian, &labelCount); err != nil {
		return StoredRecord{}, err
	}
	labels := make([]string, 0, labelCount)
	for i := uint32(0); i < labelCount; i++ {
		label, err := readString(reader)
		if err != nil {
			return StoredRecord{}, err
		}
		labels = append(labels, label)
	}
	var metadataCount uint32
	if err := binary.Read(reader, binary.BigEndian, &metadataCount); err != nil {
		return StoredRecord{}, err
	}
	metadata := make(map[string]string, metadataCount)
	for i := uint32(0); i < metadataCount; i++ {
		key, err := readString(reader)
		if err != nil {
			return StoredRecord{}, err
		}
		value, err := readString(reader)
		if err != nil {
			return StoredRecord{}, err
		}
		metadata[key] = value
	}
	if len(metadata) == 0 {
		metadata = nil
	}
	return StoredRecord{
		ID:         id,
		Code:       code,
		ValidFrom:  validFrom,
		ValidUntil: validUntil,
		Fields:     fields,
		Labels:     labels,
		Metadata:   metadata,
		CreatedAt:  time.Unix(0, createdAtNano).UTC(),
	}, nil
}

func writeStoredValue(writer io.Writer, value StoredValue) error {
	if err := writeString(writer, string(value.Type)); err != nil {
		return err
	}
	if err := writeString(writer, string(value.GeometryType)); err != nil {
		return err
	}
	switch value.Type {
	case FieldTypeGeometry:
		if value.Point == nil {
			return fmt.Errorf("missing point value for geometry field")
		}
		if err := binary.Write(writer, binary.BigEndian, value.Point.Lat); err != nil {
			return err
		}
		if err := binary.Write(writer, binary.BigEndian, value.Point.Lon); err != nil {
			return err
		}
		return writeString(writer, value.Point.Code)
	case FieldTypeString:
		return writeString(writer, derefString(value.String))
	case FieldTypeInt:
		return binary.Write(writer, binary.BigEndian, derefInt64(value.Int))
	case FieldTypeFloat:
		return binary.Write(writer, binary.BigEndian, derefFloat64(value.Float))
	case FieldTypeBool:
		return binary.Write(writer, binary.BigEndian, derefBool(value.Bool))
	case FieldTypeDateTime:
		if value.DateTime == nil {
			return binary.Write(writer, binary.BigEndian, int64(0))
		}
		return binary.Write(writer, binary.BigEndian, value.DateTime.UTC().UnixNano())
	default:
		return fmt.Errorf("unsupported stored value type %q", value.Type)
	}
}

func readStoredValue(reader io.Reader) (StoredValue, error) {
	typeName, err := readString(reader)
	if err != nil {
		return StoredValue{}, err
	}
	geometryTypeName, err := readString(reader)
	if err != nil {
		return StoredValue{}, err
	}
	fieldType := FieldType(typeName)
	geometryType := GeometryType(geometryTypeName)
	switch fieldType {
	case FieldTypeGeometry:
		var lat float64
		if err := binary.Read(reader, binary.BigEndian, &lat); err != nil {
			return StoredValue{}, err
		}
		var lon float64
		if err := binary.Read(reader, binary.BigEndian, &lon); err != nil {
			return StoredValue{}, err
		}
		code, err := readString(reader)
		if err != nil {
			return StoredValue{}, err
		}
		return StoredValue{Type: fieldType, GeometryType: geometryType, Point: &StoredPoint{Lat: lat, Lon: lon, Code: code}}, nil
	case FieldTypeString:
		value, err := readString(reader)
		if err != nil {
			return StoredValue{}, err
		}
		return StoredValue{Type: fieldType, GeometryType: geometryType, String: &value}, nil
	case FieldTypeInt:
		var value int64
		if err := binary.Read(reader, binary.BigEndian, &value); err != nil {
			return StoredValue{}, err
		}
		return StoredValue{Type: fieldType, GeometryType: geometryType, Int: &value}, nil
	case FieldTypeFloat:
		var value float64
		if err := binary.Read(reader, binary.BigEndian, &value); err != nil {
			return StoredValue{}, err
		}
		return StoredValue{Type: fieldType, GeometryType: geometryType, Float: &value}, nil
	case FieldTypeBool:
		var value bool
		if err := binary.Read(reader, binary.BigEndian, &value); err != nil {
			return StoredValue{}, err
		}
		return StoredValue{Type: fieldType, GeometryType: geometryType, Bool: &value}, nil
	case FieldTypeDateTime:
		var value int64
		if err := binary.Read(reader, binary.BigEndian, &value); err != nil {
			return StoredValue{}, err
		}
		timeValue := time.Unix(0, value).UTC()
		return StoredValue{Type: fieldType, GeometryType: geometryType, DateTime: &timeValue}, nil
	default:
		return StoredValue{}, fmt.Errorf("unsupported stored value type %q", fieldType)
	}
}

func writeOptionalTime(writer io.Writer, value *time.Time) error {
	if value == nil {
		if err := binary.Write(writer, binary.BigEndian, uint8(0)); err != nil {
			return err
		}
		return nil
	}
	if err := binary.Write(writer, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	return binary.Write(writer, binary.BigEndian, value.UTC().UnixNano())
}

func readOptionalTime(reader io.Reader) (*time.Time, error) {
	var present uint8
	if err := binary.Read(reader, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	var value int64
	if err := binary.Read(reader, binary.BigEndian, &value); err != nil {
		return nil, err
	}
	timeValue := time.Unix(0, value).UTC()
	return &timeValue, nil
}

func writeString(writer io.Writer, value string) error {
	if err := binary.Write(writer, binary.BigEndian, uint32(len(value))); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, value); err != nil {
		return err
	}
	return nil
}

func readString(reader io.Reader) (string, error) {
	var length uint32
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		return "", err
	}
	if length == 0 {
		return "", nil
	}
	buffer := make([]byte, length)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return "", err
	}
	return string(buffer), nil
}

func sortedStoredRecords(records map[string]StoredRecord) []StoredRecord {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]StoredRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, records[id])
	}
	return out
}

func sortedMetadataKeys(metadata map[string]string) []string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStoredValueKeys(values map[string]StoredValue) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func derefFloat64(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func derefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func writeAtomically(path string, write func(file *os.File) error) error {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, ".locationdb-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		_ = os.Remove(tempPath)
	}()
	if err := write(tempFile); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
