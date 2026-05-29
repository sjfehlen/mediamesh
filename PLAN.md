# MediaMesh — Implementation Plan

Current state as of 2026-05-29: core scaffold is running, login works, library
scanning works (flat), JSON serialisation fixed, configurable libraries in Settings.

This document tracks all planned backend and frontend work. Items move from
Planned → In Progress → Done. Frontend detail is intentionally deferred — backend
correctness comes first.

---

## Tier 1 — Makes It Actually Usable

### 1. Scanner: TV path parsing + audiobook folder grouping

**Problem:** Scanner treats every file as a flat item. TV shows show individual
episode files instead of being grouped. Audiobooks show individual chapter files
instead of one book per folder.

**TV parsing:**
- Parse `Show Name/Season 01/S01E01 - Title.mkv` into:
  `series="Show Name"`, `season_num=1`, `episode_num=1`, `title="Title"`, `media_type=tv_episode`
- Infer `media_type` from path depth: root folder = tv_show, season folder = tv_season, file = tv_episode
- Extract episode title from filename after the SxxExx pattern
- Store `series` field on all tv_episode items

**Audiobook grouping:**
- Treat each subfolder under `/media/audiobooks` as one book
- One `library_item` per book folder (not per chapter file)
- `title` = folder name, `file_size` = sum of all chapter files
- Store chapter count in a new `track_count` column (migration)

**File extensions to support:**

| Type | Extensions |
|---|---|
| Video | mkv, mp4, avi, m4v, mov, ts, wmv, m2ts |
| Audio | mp3, m4b, flac, ogg, aac, opus, wav |
| Ebook | epub, pdf, mobi, azw3, cbz, cbr |

**Ignored paths:** `@eaDir`, `.DS_Store`, `Thumbs.db`, `feeder.json`, files
prefixed `sample-` or `trailer-`, folders named `Extras`, `Featurettes`,
`Behind The Scenes`, `Interviews`, `Scenes`, `Shorts`, `Trailers`.

**Change detection:**
- Store `file_mtime` on each `library_item`
- On rescan, skip files where `mtime` hasn't changed
- Only reprocess new or modified files

**Status:** Planned

---

### 2. Request scopes: season/series requests → transfer batches

**Problem:** A request can only target a single `item_id`. Requesting a TV season
requires N separate requests.

**Migration (005):**
```sql
ALTER TABLE requests ADD COLUMN request_scope TEXT NOT NULL DEFAULT 'item';
  -- item | season | series
ALTER TABLE requests ADD COLUMN series_name TEXT;
ALTER TABLE requests ADD COLUMN req_season_num INTEGER;
-- item_id becomes nullable when scope != item
```

**Approval flow for season/series scope:**
1. Admin approves the request
2. Server queries source peer's catalog for all matching episodes
3. Creates one `transfer` job per episode, all linked to the same `request_id`
4. Request status = `approved` until all transfers complete, then `transferred`

**Transfer bundle progress:**
- `GET /api/requests/{id}/transfers` returns all child transfers
- Request-level progress = `SUM(bytes_done) / SUM(bytes_total)` across all transfers
- Request completes when all child transfers are `complete`
- Request fails if any transfer fails after retries (partial transfer state)

**Status:** Planned

---

### 3. Core API gaps

Endpoints needed before the frontend can be properly wired:

| Endpoint | Purpose |
|---|---|
| `GET /api/me` | Current user profile, role, quota used/remaining |
| `GET /api/stats` | Node stats — item count by type, total storage, active transfers, peer count |
| `DELETE /api/requests/{id}` | Cancel a pending request |
| `POST /api/transfers/{id}/retry` | Retry a failed transfer |
| `GET /api/requests/{id}/transfers` | All child transfers for a request (bundle view) |
| `POST /api/config/scan` | Trigger full rescan of all libraries |

**Status:** Planned

---

### 4. Disk space check before transfers

**Problem:** A large transfer can fill the disk with no warning.

**Implementation:**
- Before any transfer starts, check available bytes on the filesystem at the
  destination media root
- Required: `file_size * 1.1` (10% buffer)
- If insufficient, mark transfer `failed` with `error="insufficient_disk_space"`
  and write to audit log
- Expose available space per library root in `GET /api/stats`

**Status:** Planned

---

### 5. First-run bootstrap in the UI

**Problem:** Creating the first admin requires a curl command. New users won't
know to do this.

**Implementation:**
- Login page calls `GET /api/auth/bootstrap-status` to check if any users exist
- If zero users: show "Set up your node" form inline on the login page
  (username + password + confirm password)
- On submit, calls `POST /api/auth/bootstrap` and logs in automatically
- Once users exist, the form never shows again

**Status:** Planned

---

## Tier 2 — Makes It Solid

### 6. Catalog delta sync

**Problem:** Full catalog push on every peer connect is wasteful at scale.

**Implementation:**
- Add `catalog_version` integer to node identity (increments on any library change)
- Peers exchange versions on connect — only push items newer than the peer's
  last-known version
- New table: `catalog_checkpoints(peer_id, version, synced_at)`
- Fall back to full sync if version gap is too large (> 1000 changes)

**Status:** Planned

---

### 7. Peer heartbeat and online status

**Problem:** No way to know if a peer is reachable without trying a transfer.

**Implementation:**
- New peer API endpoint: `GET /api/peer/ping` — returns `{"status":"ok","version":N}`
- Each node pings all active peers every 60 seconds
- After 3 consecutive missed pings, mark peer `status=unreachable`
- Store `last_seen` timestamp on peers table (migration)
- UI shows green/red indicator and "last seen X minutes ago"

**Status:** Planned

---

### 8. Transfer retry + configurable concurrency

**Retry logic:**
- Failed transfers retry up to 3 times with exponential backoff (1m, 5m, 15m)
- After 3 failures, status = `failed` permanently — requires manual retry
- Store `retry_count` and `next_retry_at` on transfers table (migration)

**Configurable concurrency:**
- Add `MAX_CONCURRENT_TRANSFERS` env var (default: 2)
- Expose in Settings UI

**Transfer resume safety:**
- Before resuming from a byte offset, verify the source file hasn't changed
  by comparing file size and a HEAD request ETag
- If mismatch, restart from zero

**Status:** Planned

---

### 9. File change detection in scanner

Covered in item 1 (`file_mtime` tracking). Listed separately as it requires
a schema migration and touches the scanner's core loop.

**Migration:** Add `file_mtime` column to `library_items`.

**Status:** Planned (blocked on item 1)

---

### 10. TV hierarchy API endpoints

New endpoints to support the grouped Library UI:

```
GET /api/library/tv                         — all TV series (one entry per distinct series)
GET /api/library/tv/{series}                — all seasons for a series
GET /api/library/tv/{series}/{season_num}   — all episodes in a season
```

Each series entry includes: name, season count, episode count, poster, which
peers have it (fully, partially, or not at all).

**Status:** Planned (blocked on item 1 — needs series field populated)

---

## Tier 3 — Polish

### 11. Bandwidth throttling

- Add `MAX_TRANSFER_MBPS` env var (default: unlimited)
- Implemented as a token bucket rate limiter wrapping the transfer stream writer
- Applies per active transfer, not globally
- Expose in Settings UI

**Status:** Planned

---

### 12. Notification wiring

**Outbound webhooks are already implemented.** This item is about wiring the
remaining event types that are currently not fired:

| Event | Trigger point |
|---|---|
| `media.available` | When peer catalog sync reveals items not in local library |
| `peer.connected` | On successful peer handshake |
| `peer.revoked` | On peer revocation |
| `storage.low` | When available disk space drops below 10% |
| `request.submitted` | ✅ Already firing |
| `request.approved` | ✅ Already firing |
| `request.rejected` | ✅ Already firing |
| `transfer.complete` | ✅ Already firing |
| `transfer.failed` | ✅ Already firing |

**Home Assistant integration path:**
1. Create an API key in MediaMesh with `library:read` scope
2. Register a webhook URL pointing at an HA automation trigger
3. HA receives `transfer.complete` → triggers Jellyfin/ABS scan → sends phone notification

**Status:** Partially done — wiring remaining events is Planned

---

### 13. Metadata confidence + manual correction

**Problem:** TMDB search by title can return wrong results (e.g. "Heat" returns
the wrong film, foreign title matches English title).

**Implementation:**
- Store `metadata_confidence` float on library_items (0.0–1.0)
- Low confidence = title-only fuzzy match; high confidence = exact TMDB ID match
- New endpoint: `PATCH /api/library/{id}/metadata` — admin can set correct TMDB ID
  or OL key manually, triggering a re-fetch
- UI shows a "Fix metadata" button on item detail for admins

**Status:** Planned

---

### 14. Audit log retention cleanup

- Add `LOG_RETENTION_DAYS` env var (default: 90)
- Scheduled job runs daily: `DELETE FROM audit_log WHERE occurred_at < NOW() - retention`
- Webhook deliveries older than retention are also purged
- Never purge `user.create`, `peer.invited`, `peer.accepted` — these are permanent records

**Status:** Planned

---

### 15. DB backup endpoint

- `GET /api/admin/backup` — streams the SQLite DB file as a download
- Uses SQLite's online backup API (safe during writes)
- Requires admin session
- Documented in CLAUDE.md and architecture docs

**Status:** Planned

---

## Schema Migrations Needed

| Migration | Contents |
|---|---|
| 005 | Request scopes: `request_scope`, `series_name`, `req_season_num` on requests |
| 006 | Scanner: `file_mtime`, `track_count` on library_items |
| 007 | Peers: `last_seen` on peers; `catalog_checkpoints` table |
| 008 | Transfers: `retry_count`, `next_retry_at` on transfers |

---

## Frontend Shape (detail deferred)

Build these screens once the backend for each tier is complete:

| Screen | Depends on |
|---|---|
| First-run setup | Tier 1 item 5 |
| Library — TV grouped view | Tier 2 item 10 |
| Item detail — full metadata + request scope picker | Tier 1 items 1–2 |
| Requests — bundle view with child transfers | Tier 1 item 2 |
| Transfers — grouped by request, retry button | Tier 1 items 2–3 |
| Peers — online status, last seen, rename | Tier 2 item 7 |
| Settings — bandwidth, concurrency, retention | Tier 3 items 11, 14 |
| Node info — fingerprint, public URL, stats | Tier 1 item 3 |

---

## Open Questions

| Question | Notes |
|---|---|
| Multi-hop peering | Direct-only for v1 — revisit if users want transitive discovery |
| Peer local rename | Should renaming a peer be local-only or propagated? Leaning local-only |
| Audiobook granularity | Transfer whole book only, or allow individual chapter selection? |
| Metadata for ebooks | Open Library coverage is incomplete — fallback strategy for CBZ/CBR comics? |
| Mobile app | Out of scope for v1 — the web UI should be mobile-responsive instead |
