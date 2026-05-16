# Traffic Simulator

Simulates realistic user traffic against Model Hotel to make the dashboard feel alive (e.g. for screenshots).

Each simulated "user" picks a random model, holds a multi-turn conversation for 1-3 minutes, then switches to another model. Conversations use streaming with human-like jitter between turns.

## Usage

```bash
cd tools/traffic-sim
go run . \
  -url http://localhost:8081 \
  -key <virtual-api-key> \
  -users 8 \
  -duration 10m
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | `http://localhost:8081` | Proxy base URL |
| `-key` | *(required)* | Virtual API key |
| `-users` | `8` | Concurrent simulated users |
| `-duration` | `10m` | Total run time |
| `-conv-min` | `1m` | Min conversation duration |
| `-conv-max` | `3m` | Max conversation duration |
| `-streaming` | `true` | Use streaming responses |
| `-max-tokens` | `150` | Max tokens per response |
| `-jitter` | `true` | Random 2-8s delay between turns |
| `-models` | *(built-in list)* | Comma-separated `Provider/ModelID` list (overrides defaults) |

## Model Selection

By default, 26 curated fast/reliable models from NanoGPT and Ollama Cloud. Override with `-models`:

```bash
go run . -key $KEY -models "NanoGPT/deepseek-chat,Ollama Cloud/gemma3:4b"
```

## Error Handling

- **429/502/503**: Model goes on 5-minute cooldown, user picks another
- **400/404/422**: Model marked dead permanently, never retried
- **All models unavailable**: User pauses 30s then retries

## Output

Status printed every 10 seconds (requests, errors, dead models, top 5 models by usage). Final stats on exit.
