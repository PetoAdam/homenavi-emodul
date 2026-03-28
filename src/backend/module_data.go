package backend

import (
	"encoding/json"
	"strings"
)

type ModuleDataPartial struct {
	LastUpdate string
	Zones      []ZoneElement
}

type ZoneElement struct {
	Zone        ZoneInfo        `json:"zone"`
	Description ZoneDescription `json:"description"`
	Mode        ZoneMode        `json:"mode"`
}

type ZoneInfo struct {
	ID                 int    `json:"id"`
	DuringChange       bool   `json:"duringChange"`
	CurrentTemperature *int   `json:"currentTemperature"`
	SetTemperature     *int   `json:"setTemperature"`
	ZoneState          string `json:"zoneState"`
	Humidity           *int   `json:"humidity"`
	Flags              struct {
		RelayState string `json:"relayState"`
	} `json:"flags"`
}

type ZoneDescription struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	StyleID      int    `json:"styleId"`
	DuringChange bool   `json:"duringChange"`
}

type ZoneMode struct {
	ID             int    `json:"id"`
	ParentID       *int   `json:"parentId"`
	Mode           string `json:"mode"`
	ConstTempTime  *int   `json:"constTempTime"`
	SetTemperature *int   `json:"setTemperature"`
	ScheduleIndex  *int   `json:"scheduleIndex"`
}

func ParseModuleData(raw []byte) (*ModuleDataPartial, error) {
	var payload struct {
		LastUpdate string `json:"lastUpdate"`
		Zones      struct {
			Elements []json.RawMessage `json:"elements"`
		} `json:"zones"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	out := &ModuleDataPartial{LastUpdate: strings.TrimSpace(payload.LastUpdate)}
	for _, rawZone := range payload.Zones.Elements {
		trimmed := strings.TrimSpace(string(rawZone))
		if trimmed == "" || trimmed == "null" {
			continue
		}
		var zone ZoneElement
		if err := json.Unmarshal(rawZone, &zone); err != nil {
			return nil, err
		}
		if zone.Zone.ID <= 0 {
			continue
		}
		out.Zones = append(out.Zones, zone)
	}
	return out, nil
}

func (z ZoneElement) IsDuringChange() bool {
	return z.Zone.DuringChange || z.Description.DuringChange
}

func (m *ModuleDataPartial) ZoneByID(zoneID int) (*ZoneElement, bool) {
	if m == nil {
		return nil, false
	}
	for i := range m.Zones {
		if m.Zones[i].Zone.ID == zoneID {
			return &m.Zones[i], true
		}
	}
	return nil, false
}
