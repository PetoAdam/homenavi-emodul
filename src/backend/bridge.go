package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	emodulProtocol               = "emodul"
	emodulBridgeSchema           = "hdp.v1"
	emodulDefaultMQTTBrokerURL   = "tcp://mosquitto:1883"
	emodulDefaultSyncInterval    = 60 * time.Second
	emodulAdapterHelloTopic      = "homenavi/hdp/adapter/hello"
	emodulAdapterStatusPrefix    = "homenavi/hdp/adapter/status/"
	emodulMetadataTopicPrefix    = "homenavi/hdp/device/metadata/"
	emodulStateTopicPrefix       = "homenavi/hdp/device/state/"
	emodulEventTopicPrefix       = "homenavi/hdp/device/event/"
	emodulCommandTopicPrefix     = "homenavi/hdp/device/command/"
	emodulCommandResultTopicPref = "homenavi/hdp/device/command_result/"
)

type EmodulDeviceBridge struct {
	setup        *SetupStore
	api          *EmodulAPI
	mqttClient   mqtt.Client
	adapterID    string
	syncInterval time.Duration
	syncNow      chan struct{}

	mu      sync.Mutex
	known   map[string]zoneBridgeRef
	started bool
}

type zoneBridgeRef struct {
	ModuleUDID string
	ZoneID     int
}

type bridgeCommandEnvelope struct {
	DeviceID string         `json:"device_id"`
	Command  string         `json:"command"`
	Args     map[string]any `json:"args"`
	Corr     string         `json:"corr"`
}

type bridgeCapability struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Property  string         `json:"property"`
	ValueType string         `json:"value_type"`
	Unit      string         `json:"unit,omitempty"`
	Range     map[string]any `json:"range,omitempty"`
	Access    map[string]any `json:"access"`
}

type bridgeInput struct {
	ID           string           `json:"id"`
	Label        string           `json:"label"`
	Type         string           `json:"type"`
	CapabilityID string           `json:"capability_id"`
	Property     string           `json:"property"`
	Options      []map[string]any `json:"options,omitempty"`
	Range        map[string]any   `json:"range,omitempty"`
}

func NewEmodulDeviceBridge(setup *SetupStore) *EmodulDeviceBridge {
	return &EmodulDeviceBridge{
		setup:        setup,
		api:          &EmodulAPI{Setup: setup, Client: NewEmodulClient(nil)},
		adapterID:    strings.TrimSpace(firstNonEmpty(os.Getenv("EMODUL_ADAPTER_ID"), "emodul-integration")),
		syncInterval: emodulSyncInterval(),
		syncNow:      make(chan struct{}, 1),
		known:        map[string]zoneBridgeRef{},
	}
}

func (b *EmodulDeviceBridge) Start(ctx context.Context) error {
	if b == nil {
		return errors.New("bridge not configured")
	}
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return nil
	}
	b.started = true
	b.mu.Unlock()

	brokerURL := normalizeMQTTBrokerURL(strings.TrimSpace(firstNonEmpty(os.Getenv("MQTT_BROKER_URL"), emodulDefaultMQTTBrokerURL)))
	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(fmt.Sprintf("%s-%d", b.adapterID, time.Now().UnixNano()))
	opts.SetConnectRetry(true)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)
	opts.SetConnectTimeout(12 * time.Second)
	opts.SetKeepAlive(30 * time.Second)
	opts.OnConnect = func(cli mqtt.Client) {
		if tok := cli.Subscribe(emodulCommandTopicPrefix+emodulProtocol+"/#", 1, b.handleCommandMessage); tok.Wait() && tok.Error() != nil {
			slog.Warn("emodul bridge subscribe failed", "error", tok.Error())
		}
		b.publishHello()
		b.publishStatus("online", "connected")
		b.requestSync()
	}
	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		slog.Warn("emodul bridge mqtt disconnected", "error", err)
	}

	b.mqttClient = mqtt.NewClient(opts)
	tok := b.mqttClient.Connect()
	if tok.Wait() && tok.Error() != nil {
		return tok.Error()
	}

	go b.run(ctx)
	return nil
}

func (b *EmodulDeviceBridge) Stop() {
	if b == nil || b.mqttClient == nil {
		return
	}
	b.mqttClient.Disconnect(250)
}

func (b *EmodulDeviceBridge) run(ctx context.Context) {
	ticker := time.NewTicker(b.syncInterval)
	defer ticker.Stop()
	b.requestSync()
	for {
		select {
		case <-ctx.Done():
			b.Stop()
			return
		case <-ticker.C:
			b.syncAndReport(ctx, nil)
		case <-b.syncNow:
			b.syncAndReport(ctx, nil)
		}
	}
}

func (b *EmodulDeviceBridge) requestSync() {
	if b == nil {
		return
	}
	select {
	case b.syncNow <- struct{}{}:
	default:
	}
}

func (b *EmodulDeviceBridge) syncAndReport(ctx context.Context, corrByDevice map[string]string) {
	if b == nil {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := b.syncOnce(syncCtx, corrByDevice); err != nil {
		slog.Warn("emodul bridge sync failed", "error", err)
		b.publishStatus("degraded", err.Error())
	}
}

func (b *EmodulDeviceBridge) syncOnce(ctx context.Context, corrByDevice map[string]string) error {
	_, client, sess, err := b.bridgeSession(ctx)
	if err != nil {
		b.clearKnownDevices("not configured")
		return err
	}
	mods, err := client.ListModules(ctx, sess)
	if isUnauthorized(err) {
		_ = b.api.clearToken()
		_, client, sess, err = b.bridgeSession(ctx)
		if err != nil {
			return err
		}
		mods, err = client.ListModules(ctx, sess)
	}
	if err != nil {
		return err
	}

	current := make(map[string]zoneBridgeRef)
	for _, mod := range mods {
		moduleUDID := strings.TrimSpace(mod.UDID)
		if moduleUDID == "" {
			continue
		}
		data, err := client.GetModuleData(ctx, sess, moduleUDID)
		if isUnauthorized(err) {
			_ = b.api.clearToken()
			_, client, sess, err = b.bridgeSession(ctx)
			if err != nil {
				return err
			}
			data, err = client.GetModuleData(ctx, sess, moduleUDID)
		}
		if err != nil {
			slog.Warn("emodul module sync failed", "module_udid", moduleUDID, "error", err)
			continue
		}
		partial, err := ParseModuleData(data)
		if err != nil {
			slog.Warn("emodul module parse failed", "module_udid", moduleUDID, "error", err)
			continue
		}
		for _, zone := range partial.Zones {
			deviceID := emodulZoneDeviceID(moduleUDID, zone.Zone.ID)
			current[deviceID] = zoneBridgeRef{ModuleUDID: moduleUDID, ZoneID: zone.Zone.ID}
			b.publishZone(mod, zone, corrByDevice[deviceID])
		}
	}

	b.reconcileKnownDevices(current)
	b.publishStatus("online", fmt.Sprintf("synced %d zones", len(current)))
	return nil
}

func (b *EmodulDeviceBridge) bridgeSession(ctx context.Context) (*EmodulSettings, *EmodulClient, *EmodulSession, error) {
	settings, err := b.api.loadSettings()
	if err != nil {
		return nil, nil, nil, err
	}
	if strings.TrimSpace(settings.Username) == "" || strings.TrimSpace(settings.Password) == "" {
		return nil, nil, nil, errors.New("integration is not configured")
	}
	client := b.api.clientFor(settings)
	sess, err := b.api.ensureSession(ctx, settings, client)
	if err != nil {
		return nil, nil, nil, err
	}
	return settings, client, sess, nil
}

func (b *EmodulDeviceBridge) reconcileKnownDevices(current map[string]zoneBridgeRef) {
	b.mu.Lock()
	previous := b.known
	b.known = current
	b.mu.Unlock()

	for deviceID := range previous {
		if _, ok := current[deviceID]; ok {
			continue
		}
		b.publishRemoval(deviceID, "zone_removed")
	}
}

func (b *EmodulDeviceBridge) clearKnownDevices(reason string) {
	b.mu.Lock()
	previous := b.known
	b.known = map[string]zoneBridgeRef{}
	b.mu.Unlock()
	for deviceID := range previous {
		b.publishRemoval(deviceID, reason)
	}
	b.publishStatus("degraded", reason)
}

func (b *EmodulDeviceBridge) publishZone(mod EmodulModule, zone ZoneElement, corr string) {
	if b == nil || b.mqttClient == nil {
		return
	}
	deviceID := emodulZoneDeviceID(mod.UDID, zone.Zone.ID)
	meta := map[string]any{
		"schema":       emodulBridgeSchema,
		"type":         "metadata",
		"device_id":    deviceID,
		"protocol":     emodulProtocol,
		"manufacturer": "eModul",
		"model":        strings.TrimSpace(firstNonEmpty(mod.Type, "Zone")),
		"description":  emodulZoneDescription(mod, zone),
		"icon":         emodulZoneIcon(zone.Description.StyleID),
		"online":       emodulModuleOnline(mod),
		"ts":           time.Now().UnixMilli(),
		"capabilities": emodulZoneCapabilities(),
		"inputs":       emodulZoneInputs(),
	}
	state := map[string]any{
		"online":             emodulModuleOnline(mod),
		"power":              emodulPowerState(zone),
		"temperature":        tempTenthsToFloat(zone.Zone.CurrentTemperature),
		"target_temperature": tempTenthsToFloat(zone.Zone.SetTemperature),
		"humidity":           intPtrToValue(zone.Zone.Humidity),
		"relay":              strings.EqualFold(strings.TrimSpace(zone.Zone.Flags.RelayState), "on"),
		"zone_state":         strings.TrimSpace(zone.Zone.ZoneState),
		"zone_mode":          strings.TrimSpace(zone.Mode.Mode),
		"hold_minutes":       intPtrToValue(zone.Mode.ConstTempTime),
		"module_name":        strings.TrimSpace(mod.Name),
		"controller_status":  strings.TrimSpace(mod.ControllerStatus),
		"module_status":      strings.TrimSpace(mod.ModuleStatus),
	}
	stateEnvelope := map[string]any{
		"schema":    emodulBridgeSchema,
		"type":      "state",
		"device_id": deviceID,
		"ts":        time.Now().UnixMilli(),
		"state":     state,
	}
	if corr != "" {
		stateEnvelope["corr"] = corr
	}
	b.publishRetainedJSON(emodulMetadataTopicPrefix+deviceID, meta)
	b.publishRetainedJSON(emodulStateTopicPrefix+deviceID, stateEnvelope)
}

func (b *EmodulDeviceBridge) publishRemoval(deviceID, reason string) {
	if b == nil || b.mqttClient == nil || strings.TrimSpace(deviceID) == "" {
		return
	}
	b.publishRaw(emodulMetadataTopicPrefix+deviceID, nil, true)
	b.publishRaw(emodulStateTopicPrefix+deviceID, nil, true)
	b.publishJSON(emodulEventTopicPrefix+deviceID, false, map[string]any{
		"schema":    emodulBridgeSchema,
		"type":      "event",
		"event":     "device_removed",
		"device_id": deviceID,
		"data":      map[string]any{"reason": reason},
		"ts":        time.Now().UnixMilli(),
	})
}

func (b *EmodulDeviceBridge) publishHello() {
	b.publishJSON(emodulAdapterHelloTopic, false, map[string]any{
		"schema":      emodulBridgeSchema,
		"type":        "hello",
		"adapter_id":  b.adapterID,
		"protocol":    emodulProtocol,
		"version":     firstNonEmpty(strings.TrimSpace(os.Getenv("EMODUL_VERSION")), "dev"),
		"hdp_version": "1.0",
		"features": map[string]any{
			"supports_ack":         true,
			"supports_correlation": true,
			"supports_batch_state": false,
			"supports_pairing":     false,
		},
		"ts": time.Now().UnixMilli(),
	})
}

func (b *EmodulDeviceBridge) publishStatus(status, reason string) {
	b.publishRetainedJSON(emodulAdapterStatusPrefix+b.adapterID, map[string]any{
		"schema":     emodulBridgeSchema,
		"type":       "status",
		"adapter_id": b.adapterID,
		"protocol":   emodulProtocol,
		"status":     firstNonEmpty(strings.TrimSpace(status), "unknown"),
		"reason":     strings.TrimSpace(reason),
		"version":    firstNonEmpty(strings.TrimSpace(os.Getenv("EMODUL_VERSION")), "dev"),
		"features": map[string]any{
			"supports_pairing": false,
		},
		"ts": time.Now().UnixMilli(),
	})
}

func (b *EmodulDeviceBridge) handleCommandMessage(_ mqtt.Client, msg mqtt.Message) {
	go b.dispatchCommand(msg.Topic(), msg.Payload())
}

func (b *EmodulDeviceBridge) dispatchCommand(topic string, payload []byte) {
	var cmd bridgeCommandEnvelope
	if err := json.Unmarshal(payload, &cmd); err != nil {
		slog.Warn("emodul command decode failed", "topic", topic, "error", err)
		return
	}
	deviceID := strings.TrimSpace(cmd.DeviceID)
	if deviceID == "" && strings.HasPrefix(topic, emodulCommandTopicPrefix) {
		deviceID = strings.TrimPrefix(topic, emodulCommandTopicPrefix)
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return
	}
	corr := strings.TrimSpace(cmd.Corr)
	if corr == "" {
		corr = fmt.Sprintf("emodul-%d", time.Now().UnixNano())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.applyCommand(ctx, deviceID, cmd.Command, cmd.Args, corr); err != nil {
		slog.Warn("emodul command failed", "device_id", deviceID, "command", cmd.Command, "error", err)
		b.publishCommandResult(deviceID, corr, false, "failed", err.Error())
		return
	}
	b.publishCommandResult(deviceID, corr, true, "applied", "")
}

func (b *EmodulDeviceBridge) applyCommand(ctx context.Context, deviceID, command string, args map[string]any, corr string) error {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "refresh" {
		b.requestSync()
		return nil
	}
	if command != "set_state" {
		return fmt.Errorf("unsupported command %q", command)
	}
	moduleUDID, zoneID, ok := parseEmodulZoneDeviceID(deviceID)
	if !ok {
		return errors.New("invalid emodul zone device id")
	}
	if len(args) == 0 {
		return errors.New("missing command args")
	}

	applied := false
	if power, ok := extractPowerArg(args); ok {
		var err error
		if power {
			_, err = b.api.ZoneOn(ctx, moduleUDID, zoneID)
		} else {
			_, err = b.api.ZoneOff(ctx, moduleUDID, zoneID)
		}
		if err != nil {
			return err
		}
		applied = true
	}
	if temp, ok := extractTemperatureArg(args); ok {
		minutes := extractHoldMinutesArg(args)
		if _, err := b.api.SetConstantTemperature(ctx, moduleUDID, zoneID, temp, minutes); err != nil {
			return err
		}
		applied = true
	}
	if !applied {
		return errors.New("no supported state keys in command")
	}
	b.syncModule(ctx, moduleUDID, map[string]string{deviceID: corr})
	return nil
}

func (b *EmodulDeviceBridge) syncModule(ctx context.Context, moduleUDID string, corrByDevice map[string]string) {
	_, client, sess, err := b.bridgeSession(ctx)
	if err != nil {
		slog.Warn("emodul targeted sync failed", "module_udid", moduleUDID, "error", err)
		b.requestSync()
		return
	}
	mods, err := client.ListModules(ctx, sess)
	if err != nil {
		slog.Warn("emodul module lookup failed", "module_udid", moduleUDID, "error", err)
		b.requestSync()
		return
	}
	for _, mod := range mods {
		if strings.TrimSpace(mod.UDID) != strings.TrimSpace(moduleUDID) {
			continue
		}
		data, err := client.GetModuleData(ctx, sess, moduleUDID)
		if err != nil {
			slog.Warn("emodul targeted module fetch failed", "module_udid", moduleUDID, "error", err)
			b.requestSync()
			return
		}
		partial, err := ParseModuleData(data)
		if err != nil {
			slog.Warn("emodul targeted module parse failed", "module_udid", moduleUDID, "error", err)
			b.requestSync()
			return
		}
		for _, zone := range partial.Zones {
			deviceID := emodulZoneDeviceID(moduleUDID, zone.Zone.ID)
			b.publishZone(mod, zone, corrByDevice[deviceID])
		}
		return
	}
	b.requestSync()
}

func (b *EmodulDeviceBridge) publishCommandResult(deviceID, corr string, success bool, status, errMsg string) {
	b.publishJSON(emodulCommandResultTopicPref+deviceID, false, map[string]any{
		"schema":    emodulBridgeSchema,
		"type":      "command_result",
		"device_id": deviceID,
		"corr":      corr,
		"success":   success,
		"status":    status,
		"error":     errMsg,
		"ts":        time.Now().UnixMilli(),
	})
}

func (b *EmodulDeviceBridge) publishRetainedJSON(topic string, payload map[string]any) {
	b.publishJSONWithRetain(topic, payload, true)
}

func (b *EmodulDeviceBridge) publishJSON(topic string, retained bool, payload map[string]any) {
	b.publishJSONWithRetain(topic, payload, retained)
}

func (b *EmodulDeviceBridge) publishJSONWithRetain(topic string, payload map[string]any, retained bool) {
	if b == nil || b.mqttClient == nil || strings.TrimSpace(topic) == "" {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("emodul mqtt encode failed", "topic", topic, "error", err)
		return
	}
	b.publishRaw(topic, data, retained)
}

func (b *EmodulDeviceBridge) publishRaw(topic string, payload []byte, retained bool) {
	if b == nil || b.mqttClient == nil || strings.TrimSpace(topic) == "" {
		return
	}
	tok := b.mqttClient.Publish(topic, 1, retained, payload)
	if tok.Wait() && tok.Error() != nil {
		slog.Warn("emodul mqtt publish failed", "topic", topic, "error", tok.Error())
	}
}

func emodulZoneDeviceID(moduleUDID string, zoneID int) string {
	return fmt.Sprintf("%s/%s/zone/%d", emodulProtocol, strings.TrimSpace(moduleUDID), zoneID)
}

func parseEmodulZoneDeviceID(deviceID string) (string, int, bool) {
	trimmed := strings.Trim(strings.TrimSpace(deviceID), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 4 || parts[0] != emodulProtocol || parts[2] != "zone" {
		return "", 0, false
	}
	zoneID, err := strconv.Atoi(parts[3])
	if err != nil || zoneID <= 0 {
		return "", 0, false
	}
	if strings.TrimSpace(parts[1]) == "" {
		return "", 0, false
	}
	return parts[1], zoneID, true
}

func emodulModuleOnline(mod EmodulModule) bool {
	moduleStatus := strings.ToLower(strings.TrimSpace(mod.ModuleStatus))
	controllerStatus := strings.ToLower(strings.TrimSpace(mod.ControllerStatus))
	for _, status := range []string{moduleStatus, controllerStatus} {
		if status == "" {
			continue
		}
		if strings.Contains(status, "offline") || strings.Contains(status, "disconnect") || strings.Contains(status, "error") {
			return false
		}
	}
	return true
}

func emodulZoneDescription(mod EmodulModule, zone ZoneElement) string {
	zoneName := strings.TrimSpace(firstNonEmpty(zone.Description.Name, fmt.Sprintf("Zone %d", zone.Zone.ID)))
	moduleName := strings.TrimSpace(mod.Name)
	if moduleName == "" {
		return zoneName
	}
	return moduleName + " · " + zoneName
}

func emodulPowerState(zone ZoneElement) string {
	if strings.EqualFold(strings.TrimSpace(zone.Zone.ZoneState), "zoneOff") {
		return "off"
	}
	return "on"
}

func emodulZoneIcon(styleID int) string {
	switch styleID {
	case 1:
		return "water"
	case 4:
		return "home"
	default:
		return "thermostat"
	}
}

func emodulZoneCapabilities() []bridgeCapability {
	return []bridgeCapability{
		{
			ID:        "switch",
			Name:      "Power",
			Kind:      "actuator",
			Property:  "power",
			ValueType: "string",
			Access:    map[string]any{"read": true, "write": true, "event": true},
		},
		{
			ID:        "temperature",
			Name:      "Temperature",
			Kind:      "sensor",
			Property:  "temperature",
			ValueType: "number",
			Unit:      "°C",
			Access:    map[string]any{"read": true, "write": false, "event": true},
		},
		{
			ID:        "humidity",
			Name:      "Humidity",
			Kind:      "sensor",
			Property:  "humidity",
			ValueType: "number",
			Unit:      "%",
			Access:    map[string]any{"read": true, "write": false, "event": true},
		},
		{
			ID:        "climate.target_temperature",
			Name:      "Target Temperature",
			Kind:      "actuator",
			Property:  "target_temperature",
			ValueType: "number",
			Unit:      "°C",
			Range:     map[string]any{"min": 5, "max": 35, "step": 0.5},
			Access:    map[string]any{"read": true, "write": true, "event": true},
		},
		{
			ID:        "climate.mode",
			Name:      "Mode",
			Kind:      "sensor",
			Property:  "zone_mode",
			ValueType: "string",
			Access:    map[string]any{"read": true, "write": false, "event": true},
		},
	}
}

func emodulZoneInputs() []bridgeInput {
	return []bridgeInput{
		{
			ID:           "set_power",
			Label:        "Power",
			Type:         "select",
			CapabilityID: "switch",
			Property:     "power",
			Options: []map[string]any{
				{"value": "on", "label": "On"},
				{"value": "off", "label": "Off"},
			},
		},
		{
			ID:           "set_target_temperature",
			Label:        "Target Temperature",
			Type:         "slider",
			CapabilityID: "climate.target_temperature",
			Property:     "target_temperature",
			Range:        map[string]any{"min": 5, "max": 35, "step": 0.5},
		},
	}
}

func normalizeMQTTBrokerURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return emodulDefaultMQTTBrokerURL
	}
	if strings.HasPrefix(trimmed, "mqtt://") {
		return "tcp://" + strings.TrimPrefix(trimmed, "mqtt://")
	}
	return trimmed
}

func emodulSyncInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("EMODUL_SYNC_INTERVAL_SEC"))
	if raw == "" {
		return emodulDefaultSyncInterval
	}
	sec, err := strconv.Atoi(raw)
	if err != nil || sec < 15 {
		return emodulDefaultSyncInterval
	}
	return time.Duration(sec) * time.Second
}

func extractPowerArg(args map[string]any) (bool, bool) {
	for _, key := range []string{"power", "on", "enabled"} {
		v, ok := args[key]
		if !ok {
			continue
		}
		switch value := v.(type) {
		case bool:
			return value, true
		case string:
			lower := strings.ToLower(strings.TrimSpace(value))
			if lower == "on" || lower == "true" || lower == "1" || lower == "enabled" {
				return true, true
			}
			if lower == "off" || lower == "false" || lower == "0" || lower == "disabled" {
				return false, true
			}
		case float64:
			return value != 0, true
		}
	}
	return false, false
}

func extractTemperatureArg(args map[string]any) (float64, bool) {
	for _, key := range []string{"target_temperature", "temperature", "set_temperature"} {
		v, ok := args[key]
		if !ok {
			continue
		}
		switch value := v.(type) {
		case float64:
			return value, true
		case float32:
			return float64(value), true
		case int:
			return float64(value), true
		case int64:
			return float64(value), true
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func extractHoldMinutesArg(args map[string]any) int {
	for _, key := range []string{"hold_minutes", "minutes", "const_temp_minutes"} {
		v, ok := args[key]
		if !ok {
			continue
		}
		switch value := v.(type) {
		case float64:
			return int(value)
		case int:
			return value
		case int64:
			return int(value)
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err == nil {
				return n
			}
		}
	}
	return 0
}

func tempTenthsToFloat(v *int) any {
	if v == nil {
		return nil
	}
	return float64(*v) / 10.0
}

func intPtrToValue(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
