package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultDataPollIntervalSec = 30
	minDataPollIntervalSec     = 5
	maxDataPollIntervalSec     = 3600
)

func RegisterAPIRoutes(mux *http.ServeMux, api *EmodulAPI) {
	if api == nil {
		api = NewEmodulAPI(nil)
	}

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		st, err := api.Status(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, st)
	})

	mux.HandleFunc("/api/modules", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mods, err := api.ListModules(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"modules": mods})
	})

	mux.HandleFunc("/api/modules/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/modules/")
		path = strings.Trim(path, "/")
		if path == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		parts := strings.Split(path, "/")
		moduleUDID := parts[0]
		if moduleUDID == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if len(parts) == 1 {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			data, err := api.GetModuleData(r.Context(), moduleUDID)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
			writeRawJSON(w, http.StatusOK, data)
			return
		}

		if parts[1] != "zones" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if len(parts) == 2 {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			zones, err := api.ListZones(r.Context(), moduleUDID)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"zones": zones})
			return
		}

		if len(parts) != 4 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		zoneID, err := strconv.Atoi(parts[2])
		if err != nil || zoneID <= 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid zone id")
			return
		}
		action := parts[3]
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch action {
		case "on":
			resp, err := api.ZoneOn(r.Context(), moduleUDID, zoneID)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
			writeRawJSON(w, http.StatusOK, resp)
			return
		case "off":
			resp, err := api.ZoneOff(r.Context(), moduleUDID, zoneID)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
			writeRawJSON(w, http.StatusOK, resp)
			return
		case "set":
			var payload struct {
				TemperatureC *float64 `json:"temperature_c"`
				Temperature  *float64 `json:"temperature"`
				Minutes      *int     `json:"minutes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid json")
				return
			}
			temp := payload.TemperatureC
			if temp == nil {
				temp = payload.Temperature
			}
			if temp == nil {
				writeJSONError(w, http.StatusBadRequest, "missing temperature_c")
				return
			}
			minutes := 0
			if payload.Minutes != nil {
				minutes = *payload.Minutes
			}
			resp, err := api.SetConstantTemperature(r.Context(), moduleUDID, zoneID, *temp, minutes)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
			writeRawJSON(w, http.StatusOK, resp)
			return
		case "rename":
			var payload struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid json")
				return
			}
			resp, err := api.RenameZone(r.Context(), moduleUDID, zoneID, payload.Name)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
			writeRawJSON(w, http.StatusOK, resp)
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "code": status})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeRawJSON(w http.ResponseWriter, status int, payload []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

type EmodulAPI struct {
	Setup  *SetupStore
	Client *EmodulClient

	cacheMu         sync.RWMutex
	moduleCache     map[string][]byte
	refreshInFlight map[string]bool
	pollerOnce      sync.Once
}

func NewEmodulAPI(setup *SetupStore) *EmodulAPI {
	a := &EmodulAPI{
		Setup:           setup,
		Client:          NewEmodulClient(nil),
		moduleCache:     map[string][]byte{},
		refreshInFlight: map[string]bool{},
	}
	a.startBackgroundPoller()
	return a
}

type Status struct {
	Configured          bool  `json:"configured"`
	UserID              int64 `json:"user_id,omitempty"`
	DataPollIntervalSec int   `json:"data_poll_interval_sec"`
}

func (a *EmodulAPI) Status(ctx context.Context) (*Status, error) {
	if a == nil || a.Setup == nil {
		return nil, errors.New("setup store not configured")
	}
	settings, err := a.loadSettings()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.Username) == "" || strings.TrimSpace(settings.Password) == "" {
		return &Status{Configured: false, DataPollIntervalSec: normalizeDataPollIntervalSec(settings.DataPollIntervalSec)}, nil
	}
	return &Status{Configured: true, UserID: settings.UserID, DataPollIntervalSec: normalizeDataPollIntervalSec(settings.DataPollIntervalSec)}, nil
}

func (a *EmodulAPI) ListModules(ctx context.Context) ([]EmodulModule, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return nil, err
	}
	if settings.Username == "" || settings.Password == "" {
		return nil, errors.New("integration is not configured (missing username/password)")
	}
	client := a.clientFor(settings)
	sess, err := a.ensureSession(ctx, settings, client)
	if err != nil {
		return nil, err
	}
	mods, err := client.ListModules(ctx, sess)
	if isUnauthorized(err) {
		_ = a.clearToken()
		sess, err = a.ensureSession(ctx, settings, client)
		if err != nil {
			return nil, err
		}
		mods, err = client.ListModules(ctx, sess)
	}
	return mods, err
}

func (a *EmodulAPI) GetModuleData(ctx context.Context, moduleUDID string) ([]byte, error) {
	moduleUDID = strings.TrimSpace(moduleUDID)
	if moduleUDID == "" {
		return nil, errors.New("missing module udid")
	}
	if cached, ok := a.cachedModuleData(moduleUDID); ok {
		a.triggerModuleReload(moduleUDID)
		return cached, nil
	}

	data, err := a.fetchModuleData(ctx, moduleUDID)
	if err != nil {
		return nil, err
	}
	a.storeModuleData(moduleUDID, data)
	a.triggerModuleReload(moduleUDID)
	return data, nil
}

func (a *EmodulAPI) fetchModuleData(ctx context.Context, moduleUDID string) ([]byte, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return nil, err
	}
	client := a.clientFor(settings)
	sess, err := a.ensureSession(ctx, settings, client)
	if err != nil {
		return nil, err
	}
	data, err := client.GetModuleData(ctx, sess, moduleUDID)
	if isUnauthorized(err) {
		_ = a.clearToken()
		sess, err = a.ensureSession(ctx, settings, client)
		if err != nil {
			return nil, err
		}
		data, err = client.GetModuleData(ctx, sess, moduleUDID)
	}
	return data, err
}

type ZoneView struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	DescriptionID    int      `json:"description_id"`
	IconID           int      `json:"icon_id"`
	CurrentTempC     *float64 `json:"current_temp_c"`
	SetTempC         *float64 `json:"set_temp_c"`
	Mode             string   `json:"mode"`
	ConstTempMinutes *int     `json:"const_temp_minutes"`
	ZoneState        string   `json:"zone_state"`
	RelayState       string   `json:"relay_state"`
	Humidity         *int     `json:"humidity"`
}

func (a *EmodulAPI) ListZones(ctx context.Context, moduleUDID string) ([]ZoneView, error) {
	data, err := a.GetModuleData(ctx, moduleUDID)
	if err != nil {
		return nil, err
	}
	partial, err := ParseModuleData(data)
	if err != nil {
		return nil, err
	}
	out := make([]ZoneView, 0, len(partial.Zones))
	for _, z := range partial.Zones {
		view := ZoneView{
			ID:            z.Zone.ID,
			Name:          z.Description.Name,
			DescriptionID: z.Description.ID,
			IconID:        z.Description.StyleID,
			Mode:          z.Mode.Mode,
			ZoneState:     z.Zone.ZoneState,
			RelayState:    z.Zone.Flags.RelayState,
			Humidity:      z.Zone.Humidity,
		}
		if z.Zone.CurrentTemperature != nil {
			view.CurrentTempC = floatPtr(float64(*z.Zone.CurrentTemperature) / 10.0)
		}
		if z.Zone.SetTemperature != nil {
			view.SetTempC = floatPtr(float64(*z.Zone.SetTemperature) / 10.0)
		}
		if z.Mode.ConstTempTime != nil {
			v := *z.Mode.ConstTempTime
			view.ConstTempMinutes = &v
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *EmodulAPI) ZoneOn(ctx context.Context, moduleUDID string, zoneID int) ([]byte, error) {
	return a.postZoneCommand(ctx, moduleUDID, map[string]any{"zone": map[string]any{"id": zoneID, "zoneState": "zoneOn"}})
}

func (a *EmodulAPI) ZoneOff(ctx context.Context, moduleUDID string, zoneID int) ([]byte, error) {
	return a.postZoneCommand(ctx, moduleUDID, map[string]any{"zone": map[string]any{"id": zoneID, "zoneState": "zoneOff"}})
}

func (a *EmodulAPI) SetConstantTemperature(ctx context.Context, moduleUDID string, zoneID int, temperatureC float64, minutes int) ([]byte, error) {
	data, err := a.GetModuleData(ctx, moduleUDID)
	if err != nil {
		return nil, err
	}
	partial, err := ParseModuleData(data)
	if err != nil {
		return nil, err
	}
	e, ok := partial.ZoneByID(zoneID)
	if !ok {
		return nil, fmt.Errorf("zone %d not found", zoneID)
	}
	setTemp := int(temperatureC * 10.0)
	mode := "constantTemp"
	constTempTime := 0
	if minutes > 0 {
		mode = "timeLimit"
		constTempTime = minutes
	}
	scheduleIndex := 0
	if e.Mode.ScheduleIndex != nil {
		scheduleIndex = *e.Mode.ScheduleIndex
	}
	payload := map[string]any{
		"mode": map[string]any{
			"id":             e.Mode.ID,
			"parentId":       zoneID,
			"mode":           mode,
			"constTempTime":  constTempTime,
			"setTemperature": setTemp,
			"scheduleIndex":  scheduleIndex,
		},
	}
	return a.postZoneCommand(ctx, moduleUDID, payload)
}

func (a *EmodulAPI) RenameZone(ctx context.Context, moduleUDID string, zoneID int, name string) ([]byte, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, errors.New("zone name is required")
	}
	if len([]rune(trimmed)) > 12 {
		return nil, errors.New("zone name must be 12 characters or fewer")
	}
	data, err := a.GetModuleData(ctx, moduleUDID)
	if err != nil {
		return nil, err
	}
	partial, err := ParseModuleData(data)
	if err != nil {
		return nil, err
	}
	e, ok := partial.ZoneByID(zoneID)
	if !ok {
		return nil, fmt.Errorf("zone %d not found", zoneID)
	}
	return a.putZoneDescription(ctx, moduleUDID, zoneID, map[string]any{
		"zones_id":       zoneID,
		"description_id": e.Description.ID,
		"name":           trimmed,
		"icons_id":       e.Description.StyleID,
	})
}

func (a *EmodulAPI) postZoneCommand(ctx context.Context, moduleUDID string, payload any) ([]byte, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return nil, err
	}
	client := a.clientFor(settings)
	sess, err := a.ensureSession(ctx, settings, client)
	if err != nil {
		return nil, err
	}
	resp, err := client.ChangeZoneParameters(ctx, sess, moduleUDID, payload)
	if isUnauthorized(err) {
		_ = a.clearToken()
		sess, err = a.ensureSession(ctx, settings, client)
		if err != nil {
			return nil, err
		}
		resp, err = client.ChangeZoneParameters(ctx, sess, moduleUDID, payload)
	}
	a.triggerModuleReload(moduleUDID)
	return resp, err
}

func (a *EmodulAPI) putZoneDescription(ctx context.Context, moduleUDID string, zoneID int, payload any) ([]byte, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return nil, err
	}
	client := a.clientFor(settings)
	sess, err := a.ensureSession(ctx, settings, client)
	if err != nil {
		return nil, err
	}
	resp, err := client.UpdateZoneDescription(ctx, sess, moduleUDID, zoneID, payload)
	if isUnauthorized(err) {
		_ = a.clearToken()
		sess, err = a.ensureSession(ctx, settings, client)
		if err != nil {
			return nil, err
		}
		resp, err = client.UpdateZoneDescription(ctx, sess, moduleUDID, zoneID, payload)
	}
	a.triggerModuleReload(moduleUDID)
	return resp, err
}

func (a *EmodulAPI) startBackgroundPoller() {
	if a == nil {
		return
	}
	a.pollerOnce.Do(func() {
		go a.runBackgroundPoller()
	})
}

func (a *EmodulAPI) runBackgroundPoller() {
	for {
		interval := a.currentDataPollInterval()
		timer := time.NewTimer(interval)
		<-timer.C
		pollCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		a.pollCachedModules(pollCtx)
		cancel()
	}
}

func (a *EmodulAPI) currentDataPollInterval() time.Duration {
	settings, err := a.loadSettings()
	if err != nil {
		return time.Duration(defaultDataPollIntervalSec) * time.Second
	}
	sec := normalizeDataPollIntervalSec(settings.DataPollIntervalSec)
	return time.Duration(sec) * time.Second
}

func (a *EmodulAPI) pollCachedModules(ctx context.Context) {
	modules := a.cachedModuleIDs()
	for _, moduleUDID := range modules {
		moduleCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		data, err := a.fetchModuleData(moduleCtx, moduleUDID)
		cancel()
		if err != nil {
			slog.Warn("emodul module poll failed", "module_udid", moduleUDID, "error", err)
			continue
		}
		a.storeModuleData(moduleUDID, data)
	}
}

func (a *EmodulAPI) cachedModuleIDs() []string {
	if a == nil {
		return nil
	}
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	out := make([]string, 0, len(a.moduleCache))
	for moduleUDID := range a.moduleCache {
		out = append(out, moduleUDID)
	}
	return out
}

func (a *EmodulAPI) triggerModuleReload(moduleUDID string) {
	if a == nil {
		return
	}
	moduleUDID = strings.TrimSpace(moduleUDID)
	if moduleUDID == "" {
		return
	}
	a.cacheMu.Lock()
	if a.moduleCache == nil {
		a.moduleCache = map[string][]byte{}
	}
	if a.refreshInFlight == nil {
		a.refreshInFlight = map[string]bool{}
	}
	if a.refreshInFlight[moduleUDID] {
		a.cacheMu.Unlock()
		return
	}
	a.refreshInFlight[moduleUDID] = true
	a.cacheMu.Unlock()

	go func() {
		defer func() {
			a.cacheMu.Lock()
			delete(a.refreshInFlight, moduleUDID)
			a.cacheMu.Unlock()
		}()
		reloadCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		data, err := a.fetchModuleData(reloadCtx, moduleUDID)
		if err != nil {
			slog.Warn("emodul module reload failed", "module_udid", moduleUDID, "error", err)
			return
		}
		a.storeModuleData(moduleUDID, data)
	}()
}

func (a *EmodulAPI) cachedModuleData(moduleUDID string) ([]byte, bool) {
	if a == nil {
		return nil, false
	}
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	data, ok := a.moduleCache[moduleUDID]
	if !ok || len(data) == 0 {
		return nil, false
	}
	cloned := make([]byte, len(data))
	copy(cloned, data)
	return cloned, true
}

func (a *EmodulAPI) storeModuleData(moduleUDID string, data []byte) {
	if a == nil {
		return
	}
	moduleUDID = strings.TrimSpace(moduleUDID)
	if moduleUDID == "" || len(data) == 0 {
		return
	}
	cloned := make([]byte, len(data))
	copy(cloned, data)
	a.cacheMu.Lock()
	if a.moduleCache == nil {
		a.moduleCache = map[string][]byte{}
	}
	a.moduleCache[moduleUDID] = cloned
	a.cacheMu.Unlock()
}

func normalizeDataPollIntervalSec(raw int) int {
	if raw <= 0 {
		return defaultDataPollIntervalSec
	}
	if raw < minDataPollIntervalSec {
		return minDataPollIntervalSec
	}
	if raw > maxDataPollIntervalSec {
		return maxDataPollIntervalSec
	}
	return raw
}

func (a *EmodulAPI) loadSettings() (*EmodulSettings, error) {
	if a == nil || a.Setup == nil {
		return nil, errors.New("setup store not configured")
	}
	raw, err := a.Setup.Get()
	if err != nil {
		return nil, err
	}
	return ParseEmodulSettings(raw), nil
}

func (a *EmodulAPI) clientFor(s *EmodulSettings) *EmodulClient {
	client := NewEmodulClient(nil)
	client.BaseURL = s.BaseURL
	client.HTTP.Timeout = 15 * time.Second
	return client
}

func (a *EmodulAPI) ensureSession(ctx context.Context, s *EmodulSettings, c *EmodulClient) (*EmodulSession, error) {
	if s.UserID != 0 && strings.TrimSpace(s.Token) != "" {
		return &EmodulSession{Token: s.Token, UserID: s.UserID}, nil
	}
	sess, err := c.Authenticate(ctx, s.Username, s.Password)
	if err != nil {
		return nil, err
	}
	_ = a.persistToken(sess)
	return sess, nil
}

func (a *EmodulAPI) persistToken(sess *EmodulSession) error {
	if sess == nil || sess.UserID == 0 || strings.TrimSpace(sess.Token) == "" {
		return nil
	}
	return a.Setup.Update(func(m map[string]any) map[string]any {
		if m == nil {
			m = map[string]any{}
		}
		m["token"] = sess.Token
		m["user_id"] = sess.UserID
		return m
	})
}

func (a *EmodulAPI) clearToken() error {
	if a == nil || a.Setup == nil {
		return nil
	}
	return a.Setup.Update(func(m map[string]any) map[string]any {
		if m == nil {
			return map[string]any{}
		}
		delete(m, "token")
		delete(m, "user_id")
		return m
	})
}

func floatPtr(v float64) *float64 {
	return &v
}

func isUnauthorized(err error) bool {
	var apiErr *EmodulAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusUnauthorized
	}
	return false
}
