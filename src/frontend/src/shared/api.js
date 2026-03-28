function resolveIntegrationBasePath() {
  if (typeof window === 'undefined') return '';
  const path = window.location.pathname || '';
  const parts = path.split('/').filter(Boolean);
  const idx = parts.indexOf('integrations');
  if (idx >= 0 && parts[idx + 1]) {
    return `/${['integrations', parts[idx + 1]].join('/')}`;
  }
  return '';
}

function buildUrl(path) {
  const base = resolveIntegrationBasePath();
  if (!path.startsWith('/')) return `${base}/${path}`;
  return `${base}${path}`;
}

async function fetchJSON(path, options) {
  const resp = await fetch(buildUrl(path), options);
  if (!resp.ok) {
    let detail = '';
    try {
      detail = await resp.text();
    } catch {
      // ignore
    }
    throw new Error(detail || `Request failed (${resp.status})`);
  }
  return resp.json();
}

export async function fetchStatus() {
  return fetchJSON('/api/status', { method: 'GET' });
}

export async function fetchModules() {
  return fetchJSON('/api/modules', { method: 'GET' });
}

export async function fetchZones(moduleUdid) {
  return fetchJSON(`/api/modules/${encodeURIComponent(moduleUdid)}/zones`, { method: 'GET' });
}

export async function zoneOn(moduleUdid, zoneId) {
  return fetchJSON(`/api/modules/${encodeURIComponent(moduleUdid)}/zones/${zoneId}/on`, { method: 'POST' });
}

export async function zoneOff(moduleUdid, zoneId) {
  return fetchJSON(`/api/modules/${encodeURIComponent(moduleUdid)}/zones/${zoneId}/off`, { method: 'POST' });
}

export async function zoneSetTemperature(moduleUdid, zoneId, temperatureC, minutes = 0) {
  const payload = { temperature_c: Number(temperatureC) };
  const m = Number(minutes);
  if (Number.isFinite(m) && m > 0) payload.minutes = m;
  return fetchJSON(`/api/modules/${encodeURIComponent(moduleUdid)}/zones/${zoneId}/set`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

export async function zoneRename(moduleUdid, zoneId, name) {
  return fetchJSON(`/api/modules/${encodeURIComponent(moduleUdid)}/zones/${zoneId}/rename`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: String(name || '') })
  });
}

export async function fetchSetup() {
  return fetchJSON('/api/admin/setup', { method: 'GET' });
}

export async function saveSetup(settings) {
  return fetchJSON('/api/admin/setup', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ settings })
  });
}
