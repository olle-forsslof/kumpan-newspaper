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

# Install CA certificates, tzdata, SQLite, and libc6-compat for Go SSL support
RUN apk --no-cache add ca-certificates sqlite tzdata libc6-compat && \
    update-ca-certificates && \
    cp /etc/ssl/certs/ca-certificates.crt /etc/ssl/cert.pem

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Copy migrations
COPY --from=builder /app/migrations ./migrations/

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./main"]
