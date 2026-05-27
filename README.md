# MediaMesh

A self-hosted, decentralized media sharing mesh. Each participant runs the same Docker container. Nodes peer via invites and share a unified catalog across Jellyfin (movies, TV) and Audiobookshelf (audiobooks, ebooks). Users can request media from any peer; transfers run automatically and trigger local library rescans.

No central server. No third-party dependency. Each person controls their own node.

## Documentation

Full design doc: [projects/mediamesh/README.md](https://github.com/sjfehlen/home-infra-docs/blob/main/projects/mediamesh/README.md)

## Development

### Prerequisites

- Go 1.22+
- Node.js 20+
- Docker

### Run locally

```bash
go run ./cmd/mediamesh
```

### Frontend

```bash
cd web
npm install
npm run dev
```

### Database migrations

```bash
go run ./cmd/mediamesh migrate
```

## Docker

```bash
docker build -t mediamesh .
docker compose up
```
