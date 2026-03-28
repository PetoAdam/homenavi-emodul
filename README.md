# HomeNavi eModul Integration

This repository implements a **HomeNavi integration for eModul climate controllers** via the eModul cloud API.

It implements:

- `GET /.well-known/homenavi-integration.json` (manifest)
- An iframe UI surface (sidebar tab)
- An iframe widget surface
- A single React (Vite) codebase that can build both the tab UI and widget UI
- Basic security headers + CSP suitable for sandboxed iframes
- A manifest schema validator (`cmd/validate-manifest`)
- A GitHub Actions “verification” workflow (tests + gosec + manifest validation + docker build)

## Release security gates

- `verify.yml` is the main gate (manifest/structure checks, tests, `go vet`, `gosec`, Docker build, and Trivy scan).
- `release.yml` runs `verify.yml` as a required stage, so publish only proceeds after verification passes.
- The shared `PetoAdam/homenavi/.github/actions/integration-release@main` action also enforces central verification (`integration-verify` + `go vet` + `gosec`) during release.
- Release emits SBOM + provenance and signs published image digests keylessly with Cosign.

## Setup-first default (admin-managed)

This template defaults to a dedicated setup UI flow via manifest `ui.setup`:

```json
"ui": {
	"sidebar": { "enabled": true, "path": "/ui/" },
	"setup": { "enabled": true, "path": "/ui/setup/" }
}
```

The backend exposes `GET/PUT /api/admin/setup` (admin-only) and stores setup values in `config/integration.setup.json` by default (configurable via `INTEGRATION_SETUP_PATH`). For admin access, mount the Homenavi JWT public key and set `JWT_PUBLIC_KEY_PATH` in the integration container.

## Project layout (clean & minimal)

```
src/
	backend/     # integration backend logic (HTTP handlers, data access, etc.)
	frontend/    # integration UI (tab + widget) + Vite config
web/
	assets/      # static assets (icons, etc.)
	ui/          # build output for /ui
	widgets/     # build output for /widgets/<id>
marketplace/
	metadata.json # marketplace listing metadata
	assets/       # marketplace assets (icon, screenshots)
```

**Rule of thumb:** edit code under `src/` and treat `web/` as build output/static assets.

- `src/backend`: full integration HTTP server (routes + API + static wiring)
- `src/backend/cmd/integration`: standalone entrypoint
- `src/frontend`: tab + widget UI code

## Quick start (local)

```bash
go test ./...
go run ./cmd/validate-manifest

# Build the tab + widget UI from the single React codebase
cd src/frontend
npm install
npm run build
cd ../..

go run ./src/backend/cmd/integration
```

Then open:

- `http://localhost:8099/ui/`
- `http://localhost:8099/widgets/zones/`

## Frontend dev (React)

This template uses one React codebase to build **three entrypoints**:

- Tab UI → `web/ui/` (served at `/ui/`)
- Setup UI → `web/ui/setup/` (served at `/ui/setup/`)
- Widget UI → `web/widgets/zones/` (served at `/widgets/zones/`)

Commands (from `src/frontend`):

```bash
cd src/frontend
npm install

# dev server for tab
npm run dev:tab

# dev server for widget
npm run dev:widget

# dev server for setup
npm run dev:setup

# produce production assets into web/ui + web/ui/setup + web/widgets/zones
npm run build
```

UI preview during dev:

- Tab dev server: http://localhost:10000/tab.html
- Setup dev server: http://localhost:10002/setup.html
- Widget dev server: http://localhost:10001/widget.html

If the port changed (free-port auto-pick), use the exact URL printed by the dev server.

## Local build + run with Homenavi stack

Use this to test the integration through integration-proxy with local assets:

```bash
cd src/frontend
npm install
npm run build

cd ../..
docker build -t homenavi-integration:local .

docker run --rm -d \
	--name emodul \
	--network homenavi-network \
	homenavi-integration:local
```

Ensure the Homenavi integrations list includes:

```yaml
integrations:
	- id: emodul
		upstream: http://emodul:8099
```

Then use Admin → Integrations → “Refresh integrations” to reload the proxy registry.

## Marketplace metadata

Marketplace-specific metadata and assets live in:

- `marketplace/metadata.json`
- `marketplace/assets/`

Update the icon and images there to control how the integration appears in the marketplace.

Marketplace installs also need a Compose definition. Keep the production Compose file in
`compose/docker-compose.integration.yml` and reference it in `marketplace/metadata.json`
via `deployment_artifacts.compose.file`. Optional Helm artifacts can also be declared under
`deployment_artifacts.helm` (for example `chart_ref` and `version`). The release action embeds
deployment artifact metadata into the marketplace payload so HomeNavi can resolve the correct
artifact for the selected environment runtime.

## Local dev flow (backend + frontend)

1) Run the Go backend (serves manifest + APIs + built assets):

```bash
go run ./src/backend/cmd/integration
```

2) In another terminal, run the Vite dev server (tab or widget):

```bash
cd src/frontend
npm install
npm run dev:tab
```

The backend API runs on `http://localhost:8099` and the dev UI runs on the Vite port
(default `10000` / `10001`). The UI talks to the backend via relative `/api/...` calls.

## Docker: auto port assignment (no manual host ports)

If you run integrations with Docker Compose, **do not publish a fixed host port**.
Either omit `ports` entirely (recommended inside the Homenavi network) or use `0:8099`:

```yaml
services:
	emodul:
		image: your-org/emodul:latest
		ports:
			- "0:8099" # auto-assign host port
```

Inside the Homenavi stack, the integration proxy should use the **container port**:

```yaml
integrations:
	- id: emodul
		upstream: http://emodul:8099
```

If port 5173/5174 is in use, set `HN_PORT`:

```bash
HN_PORT=5175 npm run dev:tab
HN_PORT=5176 npm run dev:widget
```

## Docker: build + run on the Compose network

Use Docker directly (no Compose) but attach to the same network as the core stack
(replace `homenavi_default` with your Compose network name):

```bash
docker build -t homenavi-integration:local .

docker run --rm \
	--name emodul \
	--network homenavi_homenavi-network \
	homenavi-integration:local
```

Then configure the integration proxy to reach it via container DNS:

```yaml
integrations:
	- id: emodul
		upstream: http://emodul:8099

## Docker Compose (integration-proxy install)

This uses the production image and matches how the marketplace installs it:

```bash
INTEGRATIONS_ROOT=/path/to/homenavi \
  docker compose -f compose/docker-compose.integration.yml up -d
```

Set `HN_VERSION=vX.Y.Z` to pin a release tag.

## Docker Compose (local dev image)

Use this to build and run your local image against a running Homenavi stack:

```bash
HOMENAVI_ROOT=/path/to/homenavi \
  docker compose -f compose/docker-compose.dev.yml up --build
```
```

## Integration proxy installation (recommended)

1) Build or pull the integration image and run it on the Homenavi network.

2) Register the integration in Homenavi:

```yaml
integrations:
	- id: emodul
		upstream: http://emodul:8099
```

3) In the Homenavi UI, use Admin → Integrations → “Refresh integrations” to reload the registry.

## Helm installation (coming soon)

## Test checklist (local)

These commands validate the integration builds correctly:

- Run unit tests: `go test ./...`
- Validate manifest schema: `go run ./cmd/validate-manifest`
- Build UI assets: `cd src/frontend && npm install && npm run build`
- Build Docker image: `docker build -t homenavi-integration:local .`

## Environment

- `PORT` (default `8099`)

## Security notes

This server sets strict headers for iframe content:

- `Content-Security-Policy` with `connect-src 'self'` and `default-src 'none'`
- `X-Frame-Options: SAMEORIGIN` / `frame-ancestors 'self'`
- `Referrer-Policy: no-referrer`
- `X-Content-Type-Options: nosniff`

The intended host (Homenavi) should still render third-party UI inside an `<iframe sandbox>` without `allow-same-origin`.

## Icons (Font Awesome, etc.)

Homenavi **does not allow remote icon URLs** for integrations.

Use one of these:

### Option A (recommended): bundled icon under `/assets/...`

Ship an SVG/PNG in `web/assets/` and reference it portably from the manifest:

```json
{
	"ui": {
		"sidebar": {
			"icon": "/assets/icon.svg"
		}
	}
}
```

At runtime, Homenavi will serve it same-origin via the integration proxy as:

- `/integrations/<id>/assets/icon.svg`

This template includes `web/assets/icon.svg` and the default manifest uses it.

The included `icon.svg` is intentionally **monochrome white** so it works well across themes/backgrounds.

### Option B: Font Awesome token

Set `ui.sidebar.icon` to a token string:

```json
{
	"ui": {
		"sidebar": {
			"icon": "fa:sparkles"
		}
	}
}
```

Notes:

- Tokens are intentionally limited/allowlisted by the Homenavi frontend (for safety + consistency).
- Remote URLs like `https://.../icon.svg` are rejected by the verifier and dropped by the runtime registry.

If you want a “no-license-surprises” default for custom icons, keep using bundled SVGs you own (or permissively-licensed sets) and serve them from `/assets/`.
