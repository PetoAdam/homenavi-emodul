import React from 'react';
import { createRoot } from 'react-dom/client';
import '../shared/hn.css';
import {
  fetchSetup,
  saveSetup,
  fetchStatus,
  fetchModules,
  fetchZones,
  zoneOn,
  zoneOff,
  zoneSetTemperature,
  zoneRename
} from '../shared/api';
import {
  AppleToggle,
  getZoneIcon,
  MetricPill,
  StatusBadge,
  StepperInput,
  ZoneIcons,
  ZoneNameEditor,
  formatHumidity,
  formatTemp,
  relayTone,
  zoneIsEnabled,
  zoneStateTone,
} from '../shared/zones';

function isSetupPath() {
  if (typeof window === 'undefined') return false;
  const path = (window.location.pathname || '').toLowerCase();
  return path.endsWith('/ui/setup') || path.includes('/ui/setup/');
}

function SetupApp() {
  const [original, setOriginal] = React.useState({});
  const [baseURL, setBaseURL] = React.useState('https://emodul.eu/api/v1');
  const [username, setUsername] = React.useState('');
  const [password, setPassword] = React.useState('');
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [status, setStatus] = React.useState('');

  React.useEffect(() => {
    let alive = true;
    fetchSetup()
      .then((data) => {
        if (!alive) return;
        const settings = data?.settings || {};
        setOriginal(settings);
        setBaseURL(String(settings.base_url || settings.endpoint || 'https://emodul.eu/api/v1'));
        setUsername(String(settings.username || ''));
        setPassword(String(settings.password || ''));
      })
      .catch(() => {
        if (!alive) return;
        setStatus('Could not load existing setup values.');
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => { alive = false; };
  }, []);

  return (
    <div className="hn-shell">
      <div className="hn-card">
        <div className="hn-row">
          <div>
            <h1 className="hn-title">Integration Setup</h1>
            <div className="hn-subtitle">Configure defaults for this integration.</div>
          </div>
          <span className="hn-pill">setup</span>
        </div>

        <div className="hn-section">
          <div style={{ display: 'grid', gap: 12 }}>
            <label style={{ display: 'grid', gap: 6 }}>
              <span className="hn-subtitle">Base URL</span>
              <input
                className="hn-input"
                value={baseURL}
                onChange={(e) => setBaseURL(e.target.value)}
                placeholder="https://emodul.eu/api/v1"
                disabled={loading || saving}
              />
            </label>
            <label style={{ display: 'grid', gap: 6 }}>
              <span className="hn-subtitle">Username</span>
              <input
                className="hn-input"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Email or username"
                disabled={loading || saving}
              />
            </label>
            <label style={{ display: 'grid', gap: 6 }}>
              <span className="hn-subtitle">Password</span>
              <input
                className="hn-input"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Your password"
                disabled={loading || saving}
              />
            </label>
          </div>
          <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
            <button
              className="hn-btn"
              disabled={loading || saving}
              onClick={async () => {
                setSaving(true);
                setStatus('');
                try {
                  const nextBaseURL = String(baseURL || '').trim();
                  const nextUsername = String(username || '').trim();
                  const nextPassword = String(password || '').trim() || String(original.password || '');
                  const next = { ...original, base_url: nextBaseURL, username: nextUsername, password: nextPassword };

                  const prevBaseURL = String(original.base_url || original.endpoint || '').trim();
                  const prevUsername = String(original.username || '').trim();
                  const prevPassword = String(original.password || '').trim();
                  if (nextBaseURL !== prevBaseURL || nextUsername !== prevUsername || nextPassword !== prevPassword) {
                    delete next.token;
                    delete next.user_id;
                  }
                  delete next.endpoint;

                  await saveSetup(next);
                  setStatus('Setup saved.');
                } catch {
                  setStatus('Failed to save setup.');
                } finally {
                  setSaving(false);
                }
              }}
            >
              {saving ? 'Saving…' : 'Save setup'}
            </button>
          </div>
          {status ? <div className="hn-subtitle" style={{ marginTop: 10 }}>{status}</div> : null}
        </div>
      </div>
    </div>
  );
}

function TabApp() {
  const [configured, setConfigured] = React.useState(false);
  const [loading, setLoading] = React.useState(true);
  const [status, setStatus] = React.useState('');

  const [modules, setModules] = React.useState([]);
  const [selectedUDID, setSelectedUDID] = React.useState('');
  const [zones, setZones] = React.useState([]);
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

  const refreshZones = React.useCallback(async (udid) => {
    const data = await fetchZones(udid);
    setZones(data?.zones || []);
  }, []);

  React.useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const st = await fetchStatus();
        if (!alive) return;
        const ok = Boolean(st?.configured);
        setConfigured(ok);
        if (!ok) {
          setStatus('Not configured. Open the Setup page from the sidebar.');
          return;
        }
        const mods = await fetchModules();
        if (!alive) return;
        const list = mods?.modules || [];
        setModules(list);
        if (list.length > 0) {
          setSelectedUDID(String(list[0].udid || ''));
        }
      } catch (e) {
        if (!alive) return;
        setStatus(e?.message || 'Failed to load data.');
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => { alive = false; };
  }, []);

  React.useEffect(() => {
    let alive = true;
    if (!selectedUDID) {
      setZones([]);
      setZoneInputs({});
      return () => { alive = false; };
    }
    (async () => {
      try {
        const data = await fetchZones(selectedUDID);
        if (!alive) return;
        setZones(data?.zones || []);
        setZoneInputs({});
        setRenameDrafts({});
        setEditingZone(null);
      } catch (e) {
        if (!alive) return;
        setStatus(e?.message || 'Failed to load zones.');
      }
    })();
    return () => { alive = false; };
  }, [selectedUDID]);

  return (
    <div className="hn-shell">
      <div className="hn-card">
        <div className="hn-row">
          <div>
            <h1 className="hn-title">eModul</h1>
            <div className="hn-subtitle">View and control your zones.</div>
          </div>
          <span className="hn-pill">integration</span>
        </div>

        <div className="hn-section">
          {loading ? <div className="hn-subtitle">Loading…</div> : null}
          {status ? <div className="hn-subtitle" style={{ marginTop: 10 }}>{status}</div> : null}

          {configured && !loading ? (
            <div className="hn-device-stack">
              <div className="hn-device-card">
                <div className="hn-device-card-head">
                  <div>
                    <div className="hn-title" style={{ fontSize: 16 }}>Module</div>
                    <div className="hn-device-card-muted">Choose the controller you want to manage.</div>
                  </div>
                </div>
                <label style={{ display: 'grid', gap: 6 }}>
                  <select
                    className="hn-input"
                    value={selectedUDID}
                    onChange={(e) => setSelectedUDID(e.target.value)}
                    disabled={modules.length === 0}
                  >
                    {modules.map((m) => (
                      <option key={String(m.udid)} value={String(m.udid)}>
                        {String(m.name || m.udid)}
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              <div className="hn-device-stack">
                {zones.map((z) => {
                  const current = zoneInputs[z.id] || {};
                  const tempValue = current.temp ?? (z.set_temp_c == null ? '' : String(z.set_temp_c));
                  const holdEnabled = current.holdEnabled ?? false;
                  const minutesValue = current.minutes ?? '60';
                  const editing = editingZone === z.id;
                  const renameDraft = renameDrafts[z.id] ?? (z.name || `Zone ${z.id}`);
                  const ZoneIcon = getZoneIcon(z.icon_id);

                  return (
                    <div key={z.id} className="hn-device-card hn-device-card-grid">
                      <div className="hn-stack">
                        <div className="hn-device-card-head">
                          <ZoneNameEditor
                            icon={ZoneIcon}
                            value={z.name || `Zone ${z.id}`}
                            editing={editing}
                            draft={renameDraft}
                            busy={busyZone === z.id}
                            onEdit={() => {
                              setEditingZone(z.id);
                              setRenameDrafts((prev) => ({ ...prev, [z.id]: z.name || `Zone ${z.id}` }));
                            }}
                            onDraftChange={(next) => setRenameDrafts((prev) => ({ ...prev, [z.id]: next }))}
                            onCancel={() => {
                              setEditingZone(null);
                              setRenameDrafts((prev) => ({ ...prev, [z.id]: z.name || `Zone ${z.id}` }));
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
                              setBusyZone(z.id);
                              setStatus('');
                              try {
                                await zoneRename(selectedUDID, z.id, nextName);
                                await refreshZones(selectedUDID);
                                setEditingZone(null);
                              } catch (e) {
                                setStatus(e?.message || 'Failed to rename zone.');
                              } finally {
                                setBusyZone(null);
                              }
                            }}
                          />
                        </div>

                        <div className="hn-zone-grid">
                          <MetricPill icon={ZoneIcons.temperature} label="Current" value={formatTemp(z.current_temp_c)} emphasis />
                          <MetricPill icon={ZoneIcons.heat} label="Target" value={formatTemp(z.set_temp_c)} />
                          <MetricPill icon={ZoneIcons.humidity} label="Humidity" value={formatHumidity(z.humidity)} />
                        </div>

                        <div className="hn-badges">
                          <StatusBadge icon={ZoneIcons.mode} label="Mode" value={z.mode || '—'} />
                          <StatusBadge icon={ZoneIcons.state} label="State" value={z.zone_state || '—'} tone={zoneStateTone(z)} />
                          <StatusBadge icon={ZoneIcons.relay} label="Relay" value={z.relay_state || '—'} tone={relayTone(z)} />
                        </div>
                      </div>

                      <div className="hn-zone-controls-grid">
                        <StepperInput
                          icon={ZoneIcons.heat}
                          label="Target"
                          unit="°C"
                          value={tempValue}
                          min={5}
                          max={35}
                          step={0.5}
                          precision={1}
                          disabled={busyZone === z.id}
                          onChange={(next) => updateZoneInput(z.id, { temp: next })}
                        />

                        <div className="hn-zone-toggle-stack">
                          <AppleToggle
                            label="Timed hold"
                            checked={holdEnabled}
                            disabled={busyZone === z.id}
                            onChange={(checked) => updateZoneInput(z.id, { holdEnabled: checked })}
                          />
                          <AppleToggle
                            label="Enabled"
                            checked={zoneIsEnabled(z)}
                            disabled={busyZone === z.id}
                            onChange={async (nextChecked) => {
                              setBusyZone(z.id);
                              setStatus('');
                              try {
                                if (nextChecked) {
                                  await zoneOn(selectedUDID, z.id);
                                } else {
                                  await zoneOff(selectedUDID, z.id);
                                }
                                await refreshZones(selectedUDID);
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
                            disabled={busyZone === z.id || !holdEnabled}
                            onChange={(next) => updateZoneInput(z.id, { minutes: next })}
                          />
                        </div>

                        <button
                          className="hn-btn"
                          disabled={busyZone === z.id}
                          onClick={async () => {
                            const t = Number(tempValue);
                            const m = holdEnabled ? Number(minutesValue) : 0;
                            if (!Number.isFinite(t)) {
                              setStatus('Enter a target temperature.');
                              return;
                            }
                            setBusyZone(z.id);
                            setStatus('');
                            try {
                              await zoneSetTemperature(selectedUDID, z.id, t, m);
                              await refreshZones(selectedUDID);
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
                })}
                {modules.length > 0 && zones.length === 0 ? (
                  <div className="hn-subtitle">No zones found for this module.</div>
                ) : null}
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

if (isSetupPath()) {
  createRoot(document.getElementById('root')).render(<SetupApp />);
} else {
  createRoot(document.getElementById('root')).render(<TabApp />);
}
