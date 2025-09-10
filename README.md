# Newsletter Automation System

A Go-based system for automating company newsletter collection and generation through Slack integration.

## Quick Start

### Prerequisites
- Go 1.21 or later
- Git

### Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd newspaper
```

2. Install dependencies:
```bash
go mod download
```

3. Run the server:
```bash
go run cmd/server/main.go
```

The server will start on port 8080 (or the port specified in the `PORT` environment variable).

### Health Check

Test that the server is running:
```bash
curl http://localhost:8080/health
```

Should return:
```json
{"status": "ok", "service": "newsletter"}
```

## Project Structure

```
newsletter/
├── cmd/
│   └── server/          # Application entry points
│       └── main.go      # Main server application
├── internal/            # Private application code
│   ├── config/          # Configuration management
│   └── server/          # HTTP server implementation
├── templates/           # HTML templates (future)
├── static/             # Static assets (future)
├── go.mod              # Go module definition
└── README.md           # This file
```

## Configuration

The application uses environment variables for configuration:

- `PORT`: Server port (default: 8080)
- `LOG_LEVEL`: Logging level (default: info)
- `ENVIRONMENT`: Runtime environment (default: development)

## Development

### Running the Server
```bash
go run cmd/server/main.go
```

### Building
```bash
go build -o bin/newsletter cmd/server/main.go
```

### Testing
```bash
go test ./...
```

## Architecture

The application follows Go best practices with:
- **Dependency Injection**: Clean separation of concerns
- **Structured Logging**: Using Go's `slog` package
- **Configuration Management**: Environment-based configuration
- **Layered Architecture**: Clear separation between HTTP, business logic, and data layers

## Phase 1 Features (Current)

- ✅ HTTP server with health check endpoint
- ✅ Structured logging with slog
- ✅ Environment-based configuration
- ✅ Professional Go project structure

## Upcoming Features

- Database integration (SQLite)
- Slack bot integration
- Question management system
- Submission collection and storage
- HTML newsletter template system