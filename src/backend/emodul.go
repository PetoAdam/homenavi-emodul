package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultEmodulBaseURL = "https://emodul.eu/api/v1"

type EmodulSettings struct {
	BaseURL  string
	Username string
	Password string
	Token    string
	UserID   int64
}

func ParseEmodulSettings(raw map[string]any) *EmodulSettings {
	out := &EmodulSettings{BaseURL: defaultEmodulBaseURL}
	if raw == nil {
		return out
	}
	if v, ok := raw["base_url"].(string); ok {
		out.BaseURL = strings.TrimSpace(v)
	}
	if v, ok := raw["endpoint"].(string); ok {
		// Back-compat with template naming.
		if strings.TrimSpace(out.BaseURL) == defaultEmodulBaseURL {
			out.BaseURL = strings.TrimSpace(v)
		}
	}
	if v, ok := raw["username"].(string); ok {
		out.Username = strings.TrimSpace(v)
	}
	if v, ok := raw["password"].(string); ok {
		out.Password = v
	}
	if v, ok := raw["token"].(string); ok {
		out.Token = strings.TrimSpace(v)
	}
	out.UserID = anyToInt64(raw["user_id"])
	return out
}

func anyToInt64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return i
	default:
		return 0
	}
}

type EmodulSession struct {
	Token  string
	UserID int64
}

type EmodulModule struct {
	ID               int    `json:"id"`
	Default          bool   `json:"default"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	ControllerStatus string `json:"controllerStatus"`
	ModuleStatus     string `json:"moduleStatus"`
	Version          string `json:"version"`
	UDID             string `json:"udid"`
}

type EmodulClient struct {
	BaseURL string
	HTTP    *http.Client
}

func NewEmodulClient(httpClient *http.Client) *EmodulClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &EmodulClient{BaseURL: defaultEmodulBaseURL, HTTP: httpClient}
}

type EmodulAPIError struct {
	Status int
	Body   []byte
}

func (e *EmodulAPIError) Error() string {
	msg := strings.TrimSpace(string(e.Body))
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if msg == "" {
		return fmt.Sprintf("emodul api error: status %d", e.Status)
	}
	return fmt.Sprintf("emodul api error: status %d: %s", e.Status, msg)
}

func (c *EmodulClient) url(path string) string {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	p := "/" + strings.TrimLeft(path, "/")
	return base + p
}

func (c *EmodulClient) doJSON(ctx context.Context, method, path string, session *EmodulSession, body any) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		r = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if session != nil && strings.TrimSpace(session.Token) != "" {
		// Docs don’t specify the exact auth header; sending both is harmless and
		// improves compatibility.
		tok := strings.TrimSpace(session.Token)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("token", tok)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, data, &EmodulAPIError{Status: resp.StatusCode, Body: data}
	}
	return resp.StatusCode, data, nil
}

func (c *EmodulClient) Authenticate(ctx context.Context, username, password string) (*EmodulSession, error) {
	username = strings.TrimSpace(username)
	if username == "" || strings.TrimSpace(password) == "" {
		return nil, errors.New("missing username/password")
	}
	_, data, err := c.doJSON(ctx, http.MethodPost, "/authentication", nil, map[string]any{
		"username": username,
		"password": password,
	})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Token         string `json:"token"`
		UserID        int64  `json:"user_id"`
		Authenticated any    `json:"authenticated"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.Token) == "" || payload.UserID == 0 {
		return nil, fmt.Errorf("authentication did not return token/user_id")
	}
	return &EmodulSession{Token: payload.Token, UserID: payload.UserID}, nil
}

func (c *EmodulClient) ListModules(ctx context.Context, session *EmodulSession) ([]EmodulModule, error) {
	if session == nil || session.UserID == 0 || strings.TrimSpace(session.Token) == "" {
		return nil, errors.New("missing session")
	}
	_, data, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/users/%d/modules", session.UserID), session, nil)
	if err != nil {
		return nil, err
	}
	var mods []EmodulModule
	if err := json.Unmarshal(data, &mods); err != nil {
		return nil, err
	}
	return mods, nil
}

func (c *EmodulClient) GetModuleData(ctx context.Context, session *EmodulSession, moduleUDID string) ([]byte, error) {
	if session == nil || session.UserID == 0 || strings.TrimSpace(session.Token) == "" {
		return nil, errors.New("missing session")
	}
	moduleUDID = strings.TrimSpace(moduleUDID)
	if moduleUDID == "" {
		return nil, errors.New("missing module udid")
	}
	_, data, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/users/%d/modules/%s", session.UserID, moduleUDID), session, nil)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *EmodulClient) GetModuleUpdates(ctx context.Context, session *EmodulSession, moduleUDID, lastUpdate string) ([]byte, error) {
	if session == nil || session.UserID == 0 || strings.TrimSpace(session.Token) == "" {
		return nil, errors.New("missing session")
	}
	moduleUDID = strings.TrimSpace(moduleUDID)
	if moduleUDID == "" {
		return nil, errors.New("missing module udid")
	}
	lastUpdate = strings.TrimSpace(lastUpdate)
	if lastUpdate == "" {
		return c.GetModuleData(ctx, session, moduleUDID)
	}
	path := fmt.Sprintf(
		"/users/%d/modules/%s/update/data/parents/[]/alarm_ids/[]/last_update/%s",
		session.UserID,
		moduleUDID,
		url.PathEscape(lastUpdate),
	)
	_, data, err := c.doJSON(ctx, http.MethodGet, path, session, nil)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *EmodulClient) ChangeZoneParameters(ctx context.Context, session *EmodulSession, moduleUDID string, payload any) ([]byte, error) {
	if session == nil || session.UserID == 0 || strings.TrimSpace(session.Token) == "" {
		return nil, errors.New("missing session")
	}
	moduleUDID = strings.TrimSpace(moduleUDID)
	if moduleUDID == "" {
		return nil, errors.New("missing module udid")
	}
	_, data, err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/users/%d/modules/%s/zones", session.UserID, moduleUDID), session, payload)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *EmodulClient) UpdateZoneDescription(ctx context.Context, session *EmodulSession, moduleUDID string, zoneID int, payload any) ([]byte, error) {
	if session == nil || session.UserID == 0 || strings.TrimSpace(session.Token) == "" {
		return nil, errors.New("missing session")
	}
	moduleUDID = strings.TrimSpace(moduleUDID)
	if moduleUDID == "" {
		return nil, errors.New("missing module udid")
	}
	if zoneID <= 0 {
		return nil, errors.New("invalid zone id")
	}
	_, data, err := c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/users/%d/modules/%s/zones/%d", session.UserID, moduleUDID, zoneID), session, payload)
	if err != nil {
		return nil, err
	}
	return data, nil
}
