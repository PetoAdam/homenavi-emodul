import React from 'react';
import { createRoot } from 'react-dom/client';
import '../shared/hn.css';
import {
  fetchStatus,
  fetchModules,
  fetchZones,
  zoneOn,
  zoneOff,
  zoneRename,
  zoneSetTemperature,
} from '../shared/api';
import {
  AppleToggle,
  MetricPill,
  StatusBadge,
  StepperInput,
  ZoneIcons,
  ZoneNameEditor,
  formatHumidity,
  formatTemp,
  getZoneIcon,
  relayTone,
  zoneIsEnabled,
  zoneStateTone,
} from '../shared/zones';

function resolveWidgetKind() {
	if (typeof window === 'undefined') return 'overview';
	const path = (window.location.pathname || '').toLowerCase();
	return path.includes('/widgets/configure/') ? 'configure' : 'overview';
}

function useWidgetChrome() {
  React.useEffect(() => {
    if (typeof document === 'undefined') return undefined;
    const root = document.documentElement;
    const body = document.body;
    root.classList.add('hn-widget-body');
    body.classList.add('hn-widget-body');
    return () => {
      root.classList.remove('hn-widget-body');
      body.classList.remove('hn-widget-body');
    };
  }, []);
}

function WidgetLoadingState({ message = 'Loading…' }) {
  return (
    <div className="hn-loading-state" role="status" aria-live="polite">
      <span className="hn-loading-spinner" aria-hidden="true" />
      <div className="hn-loading-message">{message}</div>
    </div>
  );
}

function WidgetStatusState({ message }) {
  return <div className="hn-loading-message hn-loading-message--static">{message}</div>;
}

function selectedZoneStorageKey(moduleUDID) {
  const moduleKey = String(moduleUDID || '').trim() || 'default';
  return `homenavi.emodul.widget.configure.selectedZone.${moduleKey}`;
}

function useModuleZones() {
  const [loading, setLoading] = React.useState(true);
  const [status, setStatus] = React.useState('');
  const [moduleUDID, setModuleUDID] = React.useState('');
  const [zones, setZones] = React.useState([]);

  const refreshZones = React.useCallback(async (udid) => {
    const data = await fetchZones(String(udid));
    setZones(data?.zones || []);
  }, []);

  React.useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const st = await fetchStatus();
        if (!alive) return;
        if (!st?.configured) {
          setStatus('Not configured');
          return;
        }
        const mods = await fetchModules();
        if (!alive) return;
        const list = mods?.modules || [];
        if (!list.length) {
          setStatus('No modules');
          return;
        }
        const first = list[0];
        const udid = String(first.udid || '');
        setModuleUDID(udid);
        await refreshZones(udid);
      } catch (e) {
        if (!alive) return;
        setStatus(e?.message || 'Failed to load');
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => { alive = false; };
  }, [refreshZones]);

  return { loading, status, setStatus, moduleUDID, zones, refreshZones };
}

function OverviewWidget() {
  useWidgetChrome();
  const { loading, status, zones } = useModuleZones();

  return (
    <div className="hn-shell hn-widget-shell">
      <div className="hn-widget-surface hn-widget-pane">
        {loading ? <WidgetLoadingState /> : null}
        {!loading && status ? <WidgetStatusState message={status} /> : null}
        {!loading && !status ? (
          <div className="hn-overview-list hn-overview-list--airy">
            {zones.map((z) => {
              const ZoneIcon = getZoneIcon(z.icon_id);
              return (
                <div key={z.id} className="hn-overview-row hn-overview-row--flat">
                  <div className="hn-overview-zone">
                    <span className="hn-overview-zone__icon"><ZoneIcon /></span>
                    <div className="hn-overview-zone__name">{z.name || `Zone ${z.id}`}</div>
                  </div>
                  <div className="hn-overview-stats hn-overview-stats--flat">
                    <MetricPill icon={ZoneIcons.temperature} label="Current" value={formatTemp(z.current_temp_c)} emphasis />
                    <MetricPill icon={ZoneIcons.heat} label="Target" value={formatTemp(z.set_temp_c)} />
                    <MetricPill icon={ZoneIcons.humidity} label="Humidity" value={formatHumidity(z.humidity)} />
                  </div>
                </div>
              );
            })}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function ConfigureWidget() {
  useWidgetChrome();
  const { loading, status, setStatus, moduleUDID, zones, refreshZones } = useModuleZones();
  const [selectedZoneId, setSelectedZoneId] = React.useState('');
  const [busyZone, setBusyZone] = React.useState(null);
  const [zoneInputs, setZoneInputs] = React.useState({});
  const [renameDrafts, setRenameDrafts] = React.useState({});
  const [editingZone, setEditingZone] = React.useState(null);

  const updateZoneInput = React.useCallback((zoneId, patch) => {
    setZoneInputs((prev) => ({
      ...prev,
      [zoneId]: { ...(prev[zoneId] || {}), ...patch }
    }));
  }, []);

  React.useEffect(() => {
    if (!zones.length) {
      setSelectedZoneId('');
      return;
    }
    const zoneIds = new Set(zones.map((z) => String(z.id)));
    let nextSelected = String(selectedZoneId || '');

    if ((!nextSelected || !zoneIds.has(nextSelected)) && typeof window !== 'undefined') {
      try {
        const stored = window.localStorage.getItem(selectedZoneStorageKey(moduleUDID));
        if (stored && zoneIds.has(stored)) {
          nextSelected = stored;
        }
      } catch {
        // Ignore localStorage access issues.
      }
    }

    if (!nextSelected || !zoneIds.has(nextSelected)) {
      nextSelected = String(zones[0].id);
    }

    if (nextSelected !== String(selectedZoneId || '')) {
      setSelectedZoneId(nextSelected);
    }
  }, [moduleUDID, zones, selectedZoneId]);

  React.useEffect(() => {
    if (!moduleUDID || !selectedZoneId || typeof window === 'undefined') {
      return;
    }
    try {
      window.localStorage.setItem(selectedZoneStorageKey(moduleUDID), String(selectedZoneId));
    } catch {
      // Ignore localStorage access issues.
    }
  }, [moduleUDID, selectedZoneId]);

  const zone = zones.find((z) => String(z.id) === String(selectedZoneId));

  return (
    <div className="hn-shell hn-widget-shell">
      <div className="hn-widget-surface hn-widget-pane hn-stack">
        {loading ? <WidgetLoadingState /> : null}
        {!loading && status ? <WidgetStatusState message={status} /> : null}

        {!loading && zones.length ? (
          <div className="hn-field-inline hn-widget-toolbar">
            <select
              className="hn-input hn-compact-select"
              value={selectedZoneId}
              onChange={(e) => setSelectedZoneId(e.target.value)}
            >
              {zones.map((entry) => (
                <option key={entry.id} value={String(entry.id)}>{entry.name || `Zone ${entry.id}`}</option>
              ))}
            </select>
          </div>
        ) : null}

        {!loading && zone ? (() => {
          const current = zoneInputs[zone.id] || {};
          const tempValue = current.temp ?? (zone.set_temp_c == null ? '' : String(zone.set_temp_c));
          const holdEnabled = current.holdEnabled ?? false;
          const minutesValue = current.minutes ?? '60';
          const renameDraft = renameDrafts[zone.id] ?? (zone.name || `Zone ${zone.id}`);
          const editing = editingZone === zone.id;
          const ZoneIcon = getZoneIcon(zone.icon_id);
          return (
            <div className="hn-zone-config">
              <div className="hn-zone-hero">
                <ZoneNameEditor
                  icon={ZoneIcon}
                  value={zone.name || `Zone ${zone.id}`}
                  editing={editing}
                  draft={renameDraft}
                  busy={busyZone === zone.id}
                  onEdit={() => {
                    setEditingZone(zone.id);
                    setRenameDrafts((prev) => ({ ...prev, [zone.id]: zone.name || `Zone ${zone.id}` }));
                  }}
                  onDraftChange={(next) => setRenameDrafts((prev) => ({ ...prev, [zone.id]: next }))}
                  onCancel={() => {
                    setEditingZone(null);
                    setRenameDrafts((prev) => ({ ...prev, [zone.id]: zone.name || `Zone ${zone.id}` }));
                  }}
                  onSave={async () => {
                    const nextName = String(renameDraft || '').trim();
                    if (!nextName) {
                      setStatus('Zone name is required.');
                      return;
                    }
                    if (nextName.length > 12) {
                      setStatus('Zone name must be 12 characters or fewer.');
                      return;
                    }
                    setBusyZone(zone.id);
                    setStatus('');
                    try {
                      await zoneRename(moduleUDID, zone.id, nextName);
                      await refreshZones(moduleUDID);
                      setEditingZone(null);
                    } catch (e) {
                      setStatus(e?.message || 'Failed to rename zone.');
                    } finally {
                      setBusyZone(null);
                    }
                  }}
                />

                <div className="hn-zone-grid hn-zone-grid--metrics hn-zone-hero__metrics">
                  <MetricPill icon={ZoneIcons.temperature} label="Current" value={formatTemp(zone.current_temp_c)} emphasis />
                  <MetricPill icon={ZoneIcons.heat} label="Target" value={formatTemp(zone.set_temp_c)} />
                  <MetricPill icon={ZoneIcons.humidity} label="Humidity" value={formatHumidity(zone.humidity)} />
                </div>
              </div>

              <div className="hn-badges hn-badges--compact hn-zone-hero__meta">
                <StatusBadge icon={ZoneIcons.state} label="Zone" value={zone.zone_state || 'unknown'} tone={zoneStateTone(zone)} />
                <StatusBadge icon={ZoneIcons.mode} label="Mode" value={zone.mode || '—'} />
                <StatusBadge icon={ZoneIcons.relay} label="Relay" value={zone.relay_state || '—'} tone={relayTone(zone)} />
              </div>

              <div className="hn-zone-controls-grid hn-zone-controls-grid--flat">
                <StepperInput
                  icon={ZoneIcons.heat}
                  label="Target"
                  unit="°C"
                  value={tempValue}
                  min={5}
                  max={35}
                  step={0.5}
                  precision={1}
                  disabled={busyZone === zone.id}
                  onChange={(next) => updateZoneInput(zone.id, { temp: next })}
                />

                <div className="hn-zone-toggle-stack">
                  <AppleToggle
                    label="Timed hold"
                    checked={holdEnabled}
                    disabled={busyZone === zone.id}
                    onChange={(checked) => updateZoneInput(zone.id, { holdEnabled: checked })}
                  />
                  <AppleToggle
                    label="Enabled"
                    checked={zoneIsEnabled(zone)}
                    disabled={busyZone === zone.id}
                    onChange={async (checked) => {
                      setBusyZone(zone.id);
                      setStatus('');
                      try {
                        if (checked) await zoneOn(moduleUDID, zone.id);
                        else await zoneOff(moduleUDID, zone.id);
                        await refreshZones(moduleUDID);
                      } catch (e) {
                        setStatus(e?.message || 'Failed to update zone state.');
                      } finally {
                        setBusyZone(null);
                      }
                    }}
                  />
                </div>

                <div className={`hn-hold-slot${holdEnabled ? ' is-active' : ''}`}>
                  <StepperInput
                    icon={ZoneIcons.hold}
                    label="Hold"
                    unit="min"
                    value={minutesValue}
                    min={1}
                    max={1440}
                    step={15}
                    disabled={busyZone === zone.id || !holdEnabled}
                    onChange={(next) => updateZoneInput(zone.id, { minutes: next })}
                  />
                </div>

                <button
                  className="hn-btn"
                  disabled={busyZone === zone.id}
                  onClick={async () => {
                    const t = Number(tempValue);
                    const m = holdEnabled ? Number(minutesValue) : 0;
                    if (!Number.isFinite(t)) {
                      setStatus('Enter a target temperature.');
                      return;
                    }
                    setBusyZone(zone.id);
                    setStatus('');
                    try {
                      await zoneSetTemperature(moduleUDID, zone.id, t, m);
                      await refreshZones(moduleUDID);
                    } catch (e) {
                      setStatus(e?.message || 'Failed to set temperature.');
                    } finally {
                      setBusyZone(null);
                    }
                  }}
                >
                  Apply
                </button>
              </div>
            </div>
          );
        })() : null}
      </div>
    </div>
  );
}

function App() {
  return resolveWidgetKind() === 'configure' ? <ConfigureWidget /> : <OverviewWidget />;
}

createRoot(document.getElementById('root')).render(<App />);
