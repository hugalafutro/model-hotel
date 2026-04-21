# LLM-Proxy

A multi-provider LLM proxy that aggregates multiple OpenAI-compatible LLM providers behind a single endpoint.

## Features

### Core Functionality
- ✅ Multi-provider support (OpenAI, Anthropic, Groq, Ollama, etc.)
- ✅ Automatic model discovery from providers
- ✅ Smart parameter filtering for provider compatibility
- ✅ Vision payload support with capability detection
- ✅ Automatic failover with health checking
- ✅ Request logging and usage statistics
- ✅ Encrypted API key storage (AES-256-GCM)

### API Endpoints
- **Admin API**: `/api/*` (provider management, stats, logs)
- **Proxy API**: `/v1/*` (OpenAI-compatible chat completions, models)
- **Health Check**: `/health`

### Dashboard
- Real-time statistics and usage metrics
- Provider management with CRUD operations
- Model discovery and management
- Request logs with filtering and pagination
- Responsive design with mobile support

### Security
- AES-256-GCM encryption for provider API keys
- SHA-256 hashed proxy keys
- Admin token authentication
- Rate limiting
- Request size limits
- CORS configuration
- Security headers (CSP, HSTS, XSS protection)

### Deployment
- Docker multi-stage build
- PostgreSQL database with migrations
- Health checks
- Bind mounts for data persistence
- Environment-based configuration

## Quick Start

### Using Docker Compose (Recommended)

```bash
# Clone the repository
git clone <repository-url>
cd llm-proxy

# Set up environment
cp .env.example .env
# Edit .env with your MASTER_KEY

# Start the application
docker compose up --build

# Check logs for admin token
docker compose logs app | grep "Admin token"
```

### Manual Setup

```bash
# Install Go 1.25+
# Install Node.js 20+

# Backend setup
go mod download
go build -o server ./cmd/server/

# Frontend setup
cd web
npm install
npm run build

# Set environment variables
export MASTER_KEY="your-secret-key"
export DATABASE_URL="postgres://user:password@localhost:5432/llmproxy"

# Run the server
./server
```

## API Usage

### Using the Dashboard

1. Access the dashboard at `http://localhost:8081`
2. Login with the admin token from server logs
3. Add providers (OpenAI, Anthropic, etc.)
4. Generate proxy keys for clients
5. View usage statistics and logs

### Using the Proxy API

```bash
# Set your proxy key
export PROXY_KEY="your-proxy-key"

# List available models
curl http://localhost:8081/v1/models \
  -H "Authorization: Bearer $PROXY_KEY"

# Make chat completions
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Using the Admin API

```bash
# Set your admin token
export ADMIN_TOKEN="your-admin-token"

# List providers
curl http://localhost:8081/api/providers \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Add a provider
curl -X POST http://localhost:8081/api/providers \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "OpenAI",
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-..."
  }'

# Get statistics
curl http://localhost:8081/api/stats \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# View logs
curl http://localhost:8081/api/logs \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MASTER_KEY` | Yes | — | Master key for encrypting provider API keys |
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `PORT` | No | `:8080` | Server listen address |
| `DISCOVERY_INTERVAL` | No | `30m` | How often to auto-discover models |
| `DATA_DIR` | No | `./data` | Directory for admin token file |
| `ALLOW_HTTP_PROVIDERS` | No | `false` | Allow http:// provider URLs (for local dev) |
| `RATE_LIMIT_ENABLED` | No | `true` | Enable rate limiting |
| `MAX_REQUEST_SIZE` | No | `10485760` | Max request body size in bytes (10MB) |
| `CORS_ORIGINS` | No | `http://localhost:5173` | Allowed CORS origins |

## Architecture

```
llm-proxy/
├── cmd/server/          # Backend entry point
├── internal/
│   ├── config/         # Configuration management
│   ├── auth/           # Encryption and authentication
│   ├── admin/          # Admin token management
│   ├── provider/       # Provider operations and discovery
│   ├── model/          # Model management
│   ├── proxy/          # Proxy handler and filtering
│   ├── api/            # REST API handlers
│   ├── db/             # Database and migrations
│   └── logging/        # Request logging
├── web/                # React frontend
│   ├── src/
│   │   ├── components/  # UI components
│   │   ├── pages/       # Dashboard pages
│   │   ├── api/         # API client
│   │   └── main.tsx     # Frontend entry
│   └── package.json
├── docker-compose.yml   # Docker orchestration
├── Dockerfile           # Multi-stage build
└── test-integration.sh  # Integration tests
```

## Testing

```bash
# Run integration tests
./test-integration.sh

# Run unit tests
go test ./...

# Test frontend build
cd web && npm run build
```

## Development

### Backend Development

```bash
# Run with auto-reload
go run ./cmd/server/main.go

# Run tests
go test ./internal/...

# Build binary
go build -o server ./cmd/server/
```

### Frontend Development

```bash
cd web
npm install
npm run dev
```

### Database Migrations

```bash
# Add new migration
# Create file in internal/db/migrations/
# Format: 002_new_feature.sql

# Apply migrations automatically on server start
```

## Deployment

### Production Docker Compose

```bash
# Set production values in .env
MASTER_KEY=<strong-random-key>
DATABASE_URL=postgres://user:password@db:5432/llmproxy

# Deploy
docker compose up -d
```

### Production Considerations

- Use strong MASTER_KEY (32+ characters)
- Enable HTTPS for provider URLs
- Configure CORS origins appropriately
- Set up database backups
- Monitor logs for errors
- Scale database resources as needed

## Security

- Provider API keys encrypted at rest (AES-256-GCM)
- Admin token authentication for management API
- SHA-256 hashed proxy keys
- Rate limiting enabled by default
- Request size limits
- Security headers configured
- Input validation on all endpoints

## License

MIT License - feel free to use this project for your own purposes.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
