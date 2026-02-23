FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go.work and all module files
COPY go.work .
COPY pkg/ pkg/
COPY desktop/ desktop/
COPY fyne_mobile/ fyne_mobile/
COPY server/ server/

# Build server with static linking
WORKDIR /build/server
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /clipcascade-server .

# --- Final stage ---
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /clipcascade-server /usr/local/bin/clipcascade-server

# Create data directory
RUN mkdir -p /data/database

ENV CC_DATABASE_PATH=/data/database/clipcascade.db
ENV CC_PORT=8080

EXPOSE 8080

VOLUME ["/data"]

ENTRYPOINT ["clipcascade-server"]
