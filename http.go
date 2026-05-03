package locationdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (app *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.handleHealth)
	mux.HandleFunc("GET /v1/stores", app.handleListStores)
	mux.HandleFunc("POST /v1/stores", app.handleCreateStore)
	mux.HandleFunc("GET /v1/stores/", app.handleGetStore)
	mux.HandleFunc("POST /v1/stores/", app.handleStoreSubresource)
	return mux
}

func (app *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (app *App) handleListStores(w http.ResponseWriter, _ *http.Request) {
	catalog := app.Catalog()
	stores := make([]StoreDefinition, 0, len(catalog.Stores))
	for _, definition := range catalog.Stores {
		stores = append(stores, definition)
	}
	writeJSON(w, http.StatusOK, map[string]any{"stores": stores})
}

func (app *App) handleCreateStore(w http.ResponseWriter, r *http.Request) {
	var config StoreConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	definition, err := app.CreateStore(config)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusCreated, definition)
}

func (app *App) handleGetStore(w http.ResponseWriter, r *http.Request) {
	storeName, ok := storeNameFromPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("not found"))
		return
	}
	definition, exists := app.GetStore(storeName)
	if !exists {
		writeError(w, http.StatusNotFound, fmt.Errorf("store %q not found", storeName))
		return
	}
	writeJSON(w, http.StatusOK, definition)
}

func (app *App) handleStoreSubresource(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "stores" {
		writeError(w, http.StatusNotFound, fmt.Errorf("not found"))
		return
	}
	storeName := StoreName(parts[2])
	if parts[3] == "records" {
		var record RecordRequest
		if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := app.InsertRecord(storeName, record); err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "inserted"})
		return
	}
	if parts[3] != "queries" {
		writeError(w, http.StatusNotFound, fmt.Errorf("not found"))
		return
	}

	var request QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := app.ExecuteQuery(storeName, request)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func storeNameFromPath(path string) (StoreName, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != "stores" {
		return "", false
	}
	if parts[2] == "" {
		return "", false
	}
	return StoreName(parts[2]), true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
