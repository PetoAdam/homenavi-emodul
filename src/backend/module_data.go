package backend

import (
	"encoding/json"
)

type ModuleDataPartial struct {
	Zones []ZoneElement
}

type ZoneElement struct {
	Zone        ZoneInfo        `json:"zone"`
	Description ZoneDescription `json:"description"`
	Mode        ZoneMode        `json:"mode"`
}

type ZoneInfo struct {
	ID                 int    `json:"id"`
	CurrentTemperature *int   `json:"currentTemperature"`
	SetTemperature     *int   `json:"setTemperature"`
	ZoneState          string `json:"zoneState"`
	Humidity           *int   `json:"humidity"`
	Flags              struct {
		RelayState string `json:"relayState"`
	} `json:"flags"`
}

type ZoneDescription struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	StyleID int    `json:"styleId"`
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
		Zones struct {
			Elements []ZoneElement `json:"elements"`
		} `json:"zones"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return &ModuleDataPartial{Zones: payload.Zones.Elements}, nil
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
