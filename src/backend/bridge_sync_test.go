package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type recordedPublication struct {
	topic   string
	payload []byte
}

type recordingMQTT struct {
	mqtt.Client
	publications []recordedPublication
}

func (c *recordingMQTT) Publish(topic string, _ byte, _ bool, payload interface{}) mqtt.Token {
	data, _ := payload.([]byte)
	c.publications = append(c.publications, recordedPublication{topic: topic, payload: data})
	return completedToken{}
}

type completedToken struct{ mqtt.Token }

func (completedToken) Wait() bool   { return true }
func (completedToken) Error() error { return nil }

func TestSyncPreservesDevicesAcrossIncompleteInventory(t *testing.T) {
	modules := `[{"udid":"module-a","name":"Controller"}]`
	zones := `{"zones":{"elements":[{"zone":{"id":1}},{"zone":{"id":2}}]}}`
	moduleStatus := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/1/modules":
			_, _ = w.Write([]byte(modules))
		case "/users/1/modules/module-a":
			w.WriteHeader(moduleStatus)
			_, _ = w.Write([]byte(zones))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := NewSetupStore(filepath.Join(t.TempDir(), "setup.json"))
	settings := map[string]any{"base_url": server.URL, "username": "user", "password": "password", "token": "token", "user_id": 1}
	if err := store.Set(settings); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingMQTT{}
	bridge := NewEmodulDeviceBridge(&EmodulAPI{Setup: store})
	bridge.mqttClient = recorder
	ctx := context.Background()

	if err := bridge.syncOnce(ctx, nil); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if len(bridge.known) != 2 {
		t.Fatalf("initial sync found %d zones", len(bridge.known))
	}
	recorder.publications = nil

	modules = `[]`
	if err := bridge.syncOnce(ctx, nil); err == nil {
		t.Fatal("empty inventory should report a degraded sync")
	}
	restarted := NewEmodulDeviceBridge(&EmodulAPI{Setup: store})
	restarted.mqttClient = recorder
	if err := restarted.syncOnce(ctx, nil); err != nil {
		t.Fatalf("empty inventory on restart: %v", err)
	}
	modules = `[{"udid":"module-a","name":"Controller"}]`
	zones = `{"zones":{"elements":[{"zone":{"id":1}}]}}`
	if err := bridge.syncOnce(ctx, nil); err == nil {
		t.Fatal("partial zone inventory should report a degraded sync")
	}
	moduleStatus = http.StatusBadRequest
	if err := bridge.syncOnce(ctx, nil); err == nil {
		t.Fatal("failed module should report a degraded sync")
	}
	if err := store.Set(map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := bridge.syncOnce(ctx, nil); err == nil {
		t.Fatal("missing credentials should report a degraded sync")
	}
	if len(bridge.known) != 2 {
		t.Fatalf("incomplete syncs lost known zones: %#v", bridge.known)
	}
	for _, publication := range recorder.publications {
		if strings.HasPrefix(publication.topic, emodulEventTopicPrefix) || len(publication.payload) == 0 {
			t.Fatalf("incomplete sync published removal: %#v", publication)
		}
	}

	if err := store.Set(settings); err != nil {
		t.Fatal(err)
	}
	moduleStatus = http.StatusOK
	zones = `{"zones":{"elements":[{"zone":{"id":1}},{"zone":{"id":2}}]}}`
	if err := bridge.syncOnce(ctx, nil); err != nil {
		t.Fatalf("recovered sync: %v", err)
	}
}
