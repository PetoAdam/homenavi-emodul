package backend

import "testing"

func TestParseModuleData_SkipsNullElementsAndParsesFlags(t *testing.T) {
	raw := []byte(`{
		"lastUpdate":"2026-03-28T23:58:52.328904+01:00",
		"zones":{
			"elements":[
				null,
				{
					"zone":{"id":9036,"duringChange":true,"currentTemperature":235,"setTemperature":219,"zoneState":"noAlarm","humidity":42,"flags":{"relayState":"off"}},
					"description":{"id":9037,"name":"Adam Bedroom","styleId":4,"duringChange":false},
					"mode":{"id":9038,"mode":"constantTemp","constTempTime":60,"setTemperature":219,"scheduleIndex":0}
				}
			]
		}
	}`)

	partial, err := ParseModuleData(raw)
	if err != nil {
		t.Fatalf("ParseModuleData returned error: %v", err)
	}
	if partial.LastUpdate != "2026-03-28T23:58:52.328904+01:00" {
		t.Fatalf("unexpected lastUpdate: %q", partial.LastUpdate)
	}
	if len(partial.Zones) != 1 {
		t.Fatalf("expected 1 parsed zone, got %d", len(partial.Zones))
	}
	zone := partial.Zones[0]
	if zone.Zone.ID != 9036 {
		t.Fatalf("unexpected zone id: %d", zone.Zone.ID)
	}
	if !zone.IsDuringChange() {
		t.Fatalf("expected zone to be marked during-change")
	}
}

func TestZoneConfirmedForArgs_RequiresStableExpectedState(t *testing.T) {
	setTemp := 225
	partial := &ModuleDataPartial{Zones: []ZoneElement{{
		Zone: ZoneInfo{
			ID:             9036,
			SetTemperature: &setTemp,
			ZoneState:      "noAlarm",
		},
	}}}

	if !zoneConfirmedForArgs(partial, 9036, map[string]any{"target_temperature": 22.5}) {
		t.Fatalf("expected stable target temperature to confirm command")
	}

	partial.Zones[0].Zone.DuringChange = true
	if zoneConfirmedForArgs(partial, 9036, map[string]any{"target_temperature": 22.5}) {
		t.Fatalf("expected during-change zone to remain unconfirmed")
	}
}

func TestConfirmationCorrByDevice_OnlyReturnsCorrForConfirmedState(t *testing.T) {
	if got := confirmationCorrByDevice(false, "emodul/module/zone/1", "corr-1"); got != nil {
		t.Fatalf("expected nil corr map while command is still pending, got %#v", got)
	}

	got := confirmationCorrByDevice(true, "emodul/module/zone/1", "corr-1")
	if len(got) != 1 || got["emodul/module/zone/1"] != "corr-1" {
		t.Fatalf("unexpected corr map: %#v", got)
	}
}

func TestBridgeStoresAndReadsModuleLastUpdate(t *testing.T) {
	b := &EmodulDeviceBridge{moduleLastUpdate: map[string]string{}}
	b.storeModuleLastUpdate(" module-1 ", " 2026-03-29T00:39:59.8104+01:00 ")

	if got := b.lastKnownModuleUpdate("module-1"); got != "2026-03-29T00:39:59.8104+01:00" {
		t.Fatalf("unexpected lastUpdate: %q", got)
	}
}
