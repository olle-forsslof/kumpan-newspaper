FROM golang:1.23-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o main ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates sqlite tzdata curl && \
    addgroup -g 10001 kp && \
    adduser -D -H -u 10001 -G kp kp && \
    mkdir -p /app /data && \
    chown kp:kp /app /data

WORKDIR /app
COPY --from=builder /app/main ./main
COPY --from=builder /app/static ./static/

ENV DATABASE_PATH=/data/kp.db
USER kp
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl --fail --silent --show-error "http://127.0.0.1:${PORT:-8080}/health" || exit 1
CMD ["./main"]
