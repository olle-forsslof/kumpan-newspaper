# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies for CGO/SQLite
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with CGO enabled for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main ./cmd/server

# Runtime stage  
FROM alpine:latest

# Install CA certificates and update them
RUN apk --no-cache add ca-certificates sqlite tzdata && \
    update-ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Copy migrations
COPY --from=builder /app/migrations ./migrations/

# Set SSL certificate environment variable
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV SSL_CERT_DIR=/etc/ssl/certs

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./main"]