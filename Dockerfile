# Build stage
FROM docker.m.daocloud.io/library/golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download

# Build
COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# Runtime stage
FROM docker.m.daocloud.io/library/alpine:3.20

# Install runtime dependencies
RUN apk add --no-cache ffmpeg ca-certificates tzdata && \
    adduser -D -g '' appuser

# Copy binaries
COPY --from=builder /out/app /app
COPY --from=builder /out/worker /worker

USER appuser

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app", "health"]

EXPOSE 8000

ENTRYPOINT ["/app"]