# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/holiaokho ./cmd/holiaokho \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/holiao ./cmd/holiao

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 holiaokho
COPY --from=build /out/holiaokho /out/holiao /usr/local/bin/
USER holiaokho
VOLUME ["/data"]
ENV HOLIAOKHO_STORAGE_PATH=/data/blobs HOLIAOKHO_LISTEN=:8081
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8081/readyz || exit 1
ENTRYPOINT ["holiaokho"]
