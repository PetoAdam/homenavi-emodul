package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func RegisterAPIRoutes(mux *http.ServeMux, setup *SetupStore) {
	api := &EmodulAPI{Setup: setup, Client: NewEmodulClient(nil)}

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
}

type Status struct {
	Configured bool  `json:"configured"`
	UserID     int64 `json:"user_id,omitempty"`
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
		return &Status{Configured: false}, nil
	}
	return &Status{Configured: true, UserID: settings.UserID}, nil
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
			"id":            e.Mode.ID,
			"parentId":       zoneID,
			"mode":          mode,
			"constTempTime": constTempTime,
			"setTemperature": setTemp,
			"scheduleIndex": scheduleIndex,
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
	return resp, err
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
