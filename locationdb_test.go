package locationdb

import (
	"testing"
	"time"
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
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	bad := DefaultStoreConfig("", "")
	if err := bad.Validate(); err == nil {
		t.Fatalf("expected validation error")
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
