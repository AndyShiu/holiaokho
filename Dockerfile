# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
ARG VERSION=1.0.5
ENV LDFLAGS="-s -w -X github.com/holiaokho/holiaokho/internal/server.Version=$VERSION"
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" -o /out/holiaokho ./cmd/holiaokho \
 && CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" -o /out/holiao ./cmd/holiao

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 holiaokho \
 && mkdir -p /data && chown holiaokho:holiaokho /data
COPY --from=build /out/holiaokho /out/holiao /usr/local/bin/
USER holiaokho
# /data must exist in the image and belong to the runtime user: a named volume
# inherits the ownership of the directory it covers, and without this the
# container starts as uid 10001 against a root-owned volume and cannot write
# its blobs or its secret key. Kubernetes hides this with fsGroup; plain
# `docker compose up` does not.
VOLUME ["/data"]
ENV HOLIAOKHO_STORAGE_PATH=/data/blobs HOLIAOKHO_LISTEN=:8081
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8081/readyz || exit 1
ENTRYPOINT ["holiaokho"]
