FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /mediamesh ./cmd/mediamesh

FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite
WORKDIR /app
COPY --from=builder /mediamesh .
COPY migrations/ migrations/
EXPOSE 8080
ENTRYPOINT ["/app/mediamesh"]
