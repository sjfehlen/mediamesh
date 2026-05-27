# Claude Code — MediaMesh Context

MediaMesh is a self-hosted, decentralized media sharing application. Each participant runs the same Docker container on their own server. Nodes discover each other via invite tokens and maintain a unified catalog across all peered servers. Users can browse all available media across the mesh and request items to be transferred to their local server.

**Status:** Early development — not yet operational.

**Design doc:** [sjfehlen/home-infra-docs — projects/mediamesh/README.md](https://github.com/sjfehlen/home-infra-docs/blob/main/projects/mediamesh/README.md)

---

## Architecture at a Glance

**Backend:** Go — single binary, stdlib HTTP, no framework
**Database:** SQLite via `golang-migrate` (migrations) and `sqlc` (typed queries)
**Frontend:** React + TypeScript + Tailwind + shadcn/ui
**Auth (UI):** Argon2id passwords + server-side sessions; optional OIDC via Authentik
**Auth (peer-to-peer):** Ed25519 keypairs — each node signs outbound requests with its private key
**Container:** Single Docker image, same on every node

---

## Repo Structure

```
cmd/mediamesh/        — main entry point; wires all packages, starts HTTP server
internal/
  config/             — env-based config with validation
  db/                 — open SQLite, run migrations, expose *sql.DB
  identity/           — Ed25519 keypair: generate at first boot, load on restart
  audit/              — append-only audit log writer (used by every other package)
  users/              — user CRUD, argon2id hashing, invite tokens, OIDC upsert
  auth/               — sessions, middleware, OIDC flow
  catalog/            — filesystem scanner, media type detection, scheduled rescans
  metadata/           — TMDB (movies/TV) and Open Library (books) enrichment
  peers/              — invite/handshake, peer registry, catalog sync, request signing
  requests/           — request queue, approval flow, duplicate detection
  transfers/          — chunked HTTPS transfer engine, resumable, rescan triggers
  api/                — HTTP handlers for UI API and peer-to-peer API
migrations/           — SQL migration files (golang-migrate up/down pairs)
queries/              — sqlc .sql query files
web/                  — React + TypeScript frontend
  src/
    api/              — typed fetch client
    components/       — shared UI components
    pages/            — one file per screen
    hooks/            — shared React hooks
```

---

## Package Dependency Order

Each layer may only import packages from layers above it:

```
Layer 1 (no internal deps):  config, db, identity, audit
Layer 2 (→ layer 1):         users, auth
Layer 3 (→ layers 1-2):      catalog, metadata
Layer 4 (→ layers 1-3):      peers
Layer 5 (→ layers 1-4):      requests, transfers
Layer 6 (→ all):             api
```

**Never** import a higher layer from a lower one — this creates cycles and violates the architecture.

---

## Key Concepts

### Node Identity
Each node has a persistent Ed25519 keypair stored in `<DATA_DIR>/identity.key` and `identity.pub`. The public key fingerprint (hex SHA-256) is the node's stable ID across all peers. Never regenerate this unless intentionally rotating identity.

### Peer Auth
All peer-to-peer API calls carry a short-lived JWT in the `Authorization: Bearer` header, signed with the sender's private key. The receiver looks up the peer by key fingerprint, verifies the signature, and checks the `jti` claim against a short-lived replay cache. Do not use session tokens for peer routes.

### Audit Log
`audit.Log.Write()` is called by every package on every meaningful state change. It is append-only — no updates or deletes on `audit_log`. If you add a new action type, document it in the `action` column comment in the migration.

### Transfer Safety
Incoming file paths are validated against the node's configured media roots before any write. A `..` or absolute path in an item's `relative_path` must be rejected immediately. Write to a `.tmp` file and rename on success — never leave partial files in the media library.

### Duplicate Detection
Run before every transfer starts (in `transfers.Engine.executeTransfer`). Checks: exact path match, then size+title match. If either hits, mark the transfer `failed` with `error="duplicate_detected"` and write to audit log. Do not silently skip — the user needs visibility.

---

## Database

Migrations live in `migrations/` as numbered up/down pairs (`001_initial_schema.up.sql`, etc.). Run automatically on startup via `db.Open()`. Never edit an existing migration — add a new one.

Queries live in `queries/` as `.sql` files with `sqlc` annotations. After changing queries, run `sqlc generate` to regenerate `internal/db/`.

SQLite is opened with:
- `PRAGMA journal_mode=WAL` — concurrent reads during writes
- `PRAGMA foreign_keys=ON` — enforce FK constraints

---

## Media Types and Paths

| Mount path | Media type | Notes |
|---|---|---|
| `/media/movies` | `movie` | Flat or year-subfolder layout |
| `/media/tv` | `tv_show` / `tv_season` / `tv_episode` | Inferred from path depth |
| `/media/audiobooks` | `audiobook` | |
| `/media/kids-audiobooks` | `audiobook` | Tagged with kids flag |
| `/media/ebooks` | `ebook` | |
| `/media/kids-ebooks` | `ebook` | Tagged with kids flag |

---

## Coding Standards

- Every function that does I/O takes `context.Context` as its first argument
- Errors wrapped with `fmt.Errorf("package.Function: %w", err)` — never discard errors
- Structured logging via `log/slog` — key events at `Info`, all errors at `Error` with `"err"` key
- No panics outside of `main()` — return errors instead
- No global state — pass dependencies explicitly
- Tests required for all Layer 1 packages; encouraged elsewhere
- Frontend: all API calls go through `src/api/client.ts` — no raw `fetch` in components

---

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `NODE_NAME` | Yes | Display name for this node |
| `PUBLIC_URL` | Yes | Externally reachable HTTPS URL for this node |
| `DATA_DIR` | No (default `/data`) | Where SQLite DB and keypair are stored |
| `LISTEN_ADDR` | No (default `:8080`) | HTTP listen address |
| `JELLYFIN_URL` | No | Local Jellyfin base URL for rescan triggers |
| `JELLYFIN_API_KEY` | No | Jellyfin API key |
| `ABS_URL` | No | Local Audiobookshelf base URL |
| `ABS_API_KEY` | No | Audiobookshelf API key |
| `TMDB_API_KEY` | No | TMDB API key for movie/TV metadata |
| `OIDC_ISSUER` | No | Enable OIDC SSO — set to Authentik issuer URL |
| `OIDC_CLIENT_ID` | No | OIDC client ID |
| `OIDC_CLIENT_SECRET` | No | OIDC client secret |

---

## What Is Not Implemented Yet

Track against the Phase 1 checklist in the design doc. As of initial scaffold:

- DB layer wired but sqlc queries not yet generated
- No HTTP handlers beyond stubs
- No peer handshake logic
- No transfer engine
- Frontend is scaffolded but has no real API calls
- OIDC is config-plumbed but provider not yet initialized
