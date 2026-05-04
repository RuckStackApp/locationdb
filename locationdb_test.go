package locationdb

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	locationid "github.com/ruckstackapp/locationid/go"
)

func TestDefaultStoreConfig(t *testing.T) {
	config := DefaultStoreConfig("toronto", "/data/toronto")
	if config.Name != "toronto" {
		t.Fatalf("Name = %q, want toronto", config.Name)
	}
	if config.RootPath != "/data/toronto" {
		t.Fatalf("RootPath = %q, want /data/toronto", config.RootPath)
	}
	if config.IndexOptions.SpatialCellPrecision == 0 {
		t.Fatalf("expected default index options to be populated")
	}
}

func TestStoreConfigValidate(t *testing.T) {
	config := DefaultStoreConfig("toronto", "/data/toronto")
	config.Schema = &RecordSchema{Fields: map[string]FieldSchema{
		"name":   {Type: FieldTypeString, Required: true},
		"rating": {Type: FieldTypeFloat},
	}}
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	bad := DefaultStoreConfig("", "")
	if err := bad.Validate(); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestRecordSchemaValidate(t *testing.T) {
	schema := &RecordSchema{Fields: map[string]FieldSchema{
		"name":      {Type: FieldTypeString, Required: true, Enum: []string{"alice", "bob"}},
		"opened_at": {Type: FieldTypeDateTime},
	}}
	if err := schema.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	bad := &RecordSchema{Fields: map[string]FieldSchema{
		"rating": {Type: FieldTypeFloat, Enum: []string{"x"}},
	}}
	if err := bad.Validate(); err == nil {
		t.Fatalf("expected schema validation error")
	}
}

func TestCatalogValidate(t *testing.T) {
	catalog := Catalog{
		Stores: map[StoreName]StoreDefinition{
			"toronto": {Config: DefaultStoreConfig("toronto", "/data/toronto")},
		},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestQueryRequestValidate(t *testing.T) {
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	request := QueryRequest{
		Near:    &NearFilter{Lat: 43.65, Lon: -79.38, Radius: 2000},
		Labels:  []string{"restaurant"},
		ValidAt: &now,
		Limit:   50,
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	bad := QueryRequest{Near: &NearFilter{Lat: 0, Lon: 0, Radius: 0}}
	if err := bad.Validate(); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestQueryLanguageRequestValidate(t *testing.T) {
	request := QueryLanguageRequest{Expression: "NEAR(43.65, -79.38, 2000) AND label IN (\"restaurant\")"}
	if err := request.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if err := (QueryLanguageRequest{}).Validate(); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestRecordRequestValidateValidWindow(t *testing.T) {
	from := time.Date(2026, 5, 3, 13, 0, 0, 0, time.UTC)
	until := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	req := RecordRequest{ID: "bad", Lat: floatPtr(1), Lon: floatPtr(2), ValidFrom: &from, ValidUntil: &until}
	if err := req.Validate(); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestAppCreateStoreAndReloadCatalog(t *testing.T) {
	dataDir := t.TempDir()
	app, err := NewApp(dataDir)
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	rootPath := filepath.Join(dataDir, "stores", "toronto")
	definition, err := app.CreateStore(DefaultStoreConfig("toronto", rootPath))
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}
	if definition.Config.Name != "toronto" {
		t.Fatalf("Name = %q, want toronto", definition.Config.Name)
	}

	reloaded, err := NewApp(dataDir)
	if err != nil {
		t.Fatalf("NewApp() reload error = %v", err)
	}
	if _, ok := reloaded.GetStore("toronto"); !ok {
		t.Fatalf("expected reloaded catalog to contain store")
	}
}

func TestHTTPStoreAndQueryEndpoints(t *testing.T) {
	dataDir := t.TempDir()
	app, err := NewApp(dataDir)
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	config := DefaultStoreConfig("toronto", filepath.Join(dataDir, "stores", "toronto"))
	config.Schema = &RecordSchema{Fields: map[string]FieldSchema{
		"name":   {Type: FieldTypeString, Required: true},
		"rating": {Type: FieldTypeFloat},
	}}
	storeBody, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	storeReq := httptest.NewRequest(http.MethodPost, "/v1/stores", bytes.NewReader(storeBody))
	storeRes := httptest.NewRecorder()
	app.Handler().ServeHTTP(storeRes, storeReq)
	if storeRes.Code != http.StatusCreated {
		t.Fatalf("create store status = %d, want %d", storeRes.Code, http.StatusCreated)
	}

	codeB, err := locationid.Encode(43.7000, -79.5000, 14)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	for _, record := range []RecordRequest{
		{ID: "r1", Lat: floatPtr(43.6501), Lon: floatPtr(-79.3801), Precision: uintPtr(14), Labels: []string{"restaurant"}, ValidFrom: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), Fields: map[string]any{"name": "Cafe", "rating": 4.5}},
		{ID: "r2", Code: codeB.String(), Labels: []string{"park"}, Fields: map[string]any{"name": "Park"}},
	} {
		recordBody, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		recordReq := httptest.NewRequest(http.MethodPost, "/v1/stores/toronto/records", bytes.NewReader(recordBody))
		recordRes := httptest.NewRecorder()
		app.Handler().ServeHTTP(recordRes, recordReq)
		if recordRes.Code != http.StatusCreated {
			t.Fatalf("insert record status = %d, want %d", recordRes.Code, http.StatusCreated)
		}
	}

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	queryBody, err := json.Marshal(QueryRequest{
		Near:    &NearFilter{Lat: 43.65, Lon: -79.38, Radius: 2000},
		Labels:  []string{"restaurant"},
		ValidAt: &now,
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	queryReq := httptest.NewRequest(http.MethodPost, "/v1/stores/toronto/queries", bytes.NewReader(queryBody))
	queryRes := httptest.NewRecorder()
	app.Handler().ServeHTTP(queryRes, queryReq)
	if queryRes.Code != http.StatusOK {
		t.Fatalf("query status = %d, want %d", queryRes.Code, http.StatusOK)
	}

	var response QueryResponse
	if err := json.Unmarshal(queryRes.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("query status field = %q, want ok", response.Status)
	}
	if len(response.Plan.Strategy) == 0 {
		t.Fatalf("expected planned strategy steps")
	}
	if len(response.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(response.Results))
	}
	if response.Results[0].Record.ID != "r1" {
		t.Fatalf("result id = %q, want r1", response.Results[0].Record.ID)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "stores", "toronto", "records.ldb")); err != nil {
		t.Fatalf("records.ldb stat error = %v", err)
	}
}

func TestQueryValidAtFiltering(t *testing.T) {
	dataDir := t.TempDir()
	app, err := NewApp(dataDir)
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	rootPath := filepath.Join(dataDir, "stores", "toronto")
	if _, err := app.CreateStore(DefaultStoreConfig("toronto", rootPath)); err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}
	for _, record := range []RecordRequest{
		{ID: "old", Lat: floatPtr(43.6501), Lon: floatPtr(-79.3801), Precision: uintPtr(14), Labels: []string{"restaurant"}, ValidUntil: timePtr(time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC))},
		{ID: "current", Lat: floatPtr(43.6502), Lon: floatPtr(-79.3802), Precision: uintPtr(14), Labels: []string{"restaurant"}, ValidFrom: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))},
	} {
		if err := app.InsertRecord("toronto", record); err != nil {
			t.Fatalf("InsertRecord() error = %v", err)
		}
	}
	validAt := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	response, err := app.ExecuteQuery("toronto", QueryRequest{
		Near:    &NearFilter{Lat: 43.65, Lon: -79.38, Radius: 2000},
		Labels:  []string{"restaurant"},
		ValidAt: &validAt,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ExecuteQuery() error = %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Record.ID != "current" {
		t.Fatalf("unexpected valid_at results: %#v", response.Results)
	}
}

func TestInsertRecordSchemaValidation(t *testing.T) {
	dataDir := t.TempDir()
	app, err := NewApp(dataDir)
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	config := DefaultStoreConfig("toronto", filepath.Join(dataDir, "stores", "toronto"))
	config.Schema = &RecordSchema{Fields: map[string]FieldSchema{
		"name":      {Type: FieldTypeString, Required: true},
		"opened_at": {Type: FieldTypeDateTime},
	}}
	if _, err := app.CreateStore(config); err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}
	if err := app.InsertRecord("toronto", RecordRequest{ID: "bad", Lat: floatPtr(43.6501), Lon: floatPtr(-79.3801), Precision: uintPtr(14)}); err == nil {
		t.Fatalf("expected missing required field error")
	}
	if err := app.InsertRecord("toronto", RecordRequest{ID: "bad2", Lat: floatPtr(43.6501), Lon: floatPtr(-79.3801), Precision: uintPtr(14), Fields: map[string]any{"name": 1}}); err == nil {
		t.Fatalf("expected field type error")
	}
}

func TestRebuildIndexFromStoredRecords(t *testing.T) {
	dataDir := t.TempDir()
	app, err := NewApp(dataDir)
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	rootPath := filepath.Join(dataDir, "stores", "toronto")
	if _, err := app.CreateStore(DefaultStoreConfig("toronto", rootPath)); err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}
	if err := app.InsertRecord("toronto", RecordRequest{
		ID: "r1", Lat: floatPtr(43.6501), Lon: floatPtr(-79.3801), Precision: uintPtr(14), Labels: []string{"restaurant"},
	}); err != nil {
		t.Fatalf("InsertRecord() error = %v", err)
	}

	if err := os.Remove(filepath.Join(rootPath, "index.lidx")); err != nil {
		t.Fatalf("remove index error = %v", err)
	}

	reloaded, err := NewApp(dataDir)
	if err != nil {
		t.Fatalf("NewApp() reload error = %v", err)
	}

	response, err := reloaded.ExecuteQuery("toronto", QueryRequest{
		Near:   &NearFilter{Lat: 43.65, Lon: -79.38, Radius: 2000},
		Labels: []string{"restaurant"},
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ExecuteQuery() error = %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Record.ID != "r1" {
		t.Fatalf("unexpected rebuilt query results: %#v", response.Results)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func uintPtr(value uint) *uint {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}
