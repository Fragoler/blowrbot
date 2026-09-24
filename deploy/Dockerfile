# syntax=docker/dockerfile:1
# Build context is the repository root:
#   docker build -f deploy/Dockerfile -t loudbot .

FROM golang:1.26.5-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG VERSION=dev
# CGO off makes the binary static, so the runtime stage needs no libc.
# The migrations are embedded, so the image carries its own schema.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/loudbot ./cmd/loudbot

FROM alpine:3.22
RUN adduser -D -u 10001 loudbot

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/loudbot /usr/local/bin/loudbot

USER loudbot
WORKDIR /app

ENTRYPOINT ["loudbot"]
CMD ["-config", "/app/config.toml"]
