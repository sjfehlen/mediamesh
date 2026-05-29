FROM node:20-alpine AS frontend
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist ./internal/api/dist
ARG GIT_SHA=dev
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags "-X main.Version=${GIT_SHA}" -o /mediamesh ./cmd/mediamesh

FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite
WORKDIR /app
COPY --from=builder /mediamesh .
COPY migrations/ migrations/
EXPOSE 8080
ENTRYPOINT ["/app/mediamesh"]
