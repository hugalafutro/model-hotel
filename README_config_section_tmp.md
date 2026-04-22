## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MASTER_KEY` | Yes | — | Master key for encrypting provider API keys |
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `PORT` | No | `:8080` | Server listen address |
| `DATA_DIR` | No | `./data` | Directory for admin token file |
| `ALLOW_HTTP_PROVIDERS` | No | `false` | Allow http:// provider URLs (for local dev) |
| `RATE_LIMIT_ENABLED` | No | `true` | Enable rate limiting |
| `MAX_REQUEST_SIZE` | No | `10485760` | Max request body size in bytes (10MB) |
| `CORS_ORIGINS` | No | `http://localhost:5173` | Allowed CORS origins |

Discovery interval is configured via the Settings UI (stored in the database) rather than an environment variable.
