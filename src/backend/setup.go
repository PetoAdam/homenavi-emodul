package backend

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type SetupAPI struct {
	Store *SetupStore
	Admin *AdminAuth
}

func NewSetupAPI(store *SetupStore, admin *AdminAuth) *SetupAPI {
	return &SetupAPI{Store: store, Admin: admin}
}

func (s *SetupAPI) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/setup", s.handleSetup)
}

func (s *SetupAPI) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.Admin == nil || !s.Admin.RequireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		payload, err := s.Store.Get()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": payload})
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if payload.Settings == nil {
		payload.Settings = map[string]any{}
	}
	if err := s.Store.Set(payload.Settings); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type SetupStore struct {
	path string
	mu   sync.Mutex
}

func NewSetupStore(path string) *SetupStore {
	return &SetupStore{path: path}
}

func DefaultSetupPath() string {
	if v := strings.TrimSpace(os.Getenv("INTEGRATION_SETUP_PATH")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("INTEGRATIONS_SETUP_PATH")); v != "" {
		return v
	}
	return filepath.Join("config", "integration.setup.json")
}

func (s *SetupStore) Get() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked()
}

func (s *SetupStore) Set(values map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveUnlocked(values)
}

func (s *SetupStore) loadUnlocked() (map[string]any, error) {
	if strings.TrimSpace(s.path) == "" {
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return map[string]any{}, nil
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

func (s *SetupStore) saveUnlocked(values map[string]any) error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
