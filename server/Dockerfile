FROM golang:1.25-alpine AS builder

WORKDIR /build

# 只复制 server 真正依赖的模块
COPY core/ core/
COPY server/ server/

# GOWORK=off：跳过 go.work，只用 server/go.mod（server 已有 replace ../core）
WORKDIR /build/server
RUN CGO_ENABLED=0 GOWORK=off go build -ldflags="-s -w" -o /clipcascade-server .

# --- Final stage ---
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /clipcascade-server /usr/local/bin/clipcascade-server

RUN mkdir -p /data/database

ENV CC_DATABASE_PATH=/data/database/clipcascade.db
ENV CC_PORT=8080

EXPOSE 8080

VOLUME ["/data"]

ENTRYPOINT ["clipcascade-server"]
