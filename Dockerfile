# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build


FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS backend

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /src/web/dist /src/internal/frontend/dist

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/jianmen ./cmd/bastion-core


FROM alpine:3.23

LABEL org.opencontainers.image.source="https://github.com/zhang-guo-wen/Jianmen"
LABEL org.opencontainers.image.description="Jianmen bastion host"
LABEL org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 jianmen \
    && adduser -S -D -H -u 10001 -G jianmen jianmen \
    && mkdir -p /app/data \
    && chown -R jianmen:jianmen /app

WORKDIR /app

COPY --from=backend --chown=jianmen:jianmen /out/jianmen /app/jianmen
COPY --chown=jianmen:jianmen config.docker.json /app/config.json

USER jianmen

VOLUME ["/app/data"]

EXPOSE 47100 47102 33060

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:47100/api/init/status || exit 1

STOPSIGNAL SIGTERM

ENTRYPOINT ["/app/jianmen"]
CMD ["-config", "/app/config.json"]
