# Traffic Simulator

Simulates realistic user traffic against Model Hotel to make the dashboard feel alive (e.g. for screenshots).

Each simulated "user" picks a random model, holds a multi-turn conversation for 1-3 minutes, then switches to another model. Conversations use streaming with human-like jitter between turns.

## Usage

```bash
cd tools/traffic-sim
go run . \
  -url http://localhost:8081 \
  -key <virtual-api-key> \
  -providers "Ollama Cloud,NanoGPT" \
  -users 8 \
  -duration 5m \
  -max-tokens-min 10 \
  -max-tokens-max 500
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | `http://localhost:8081` | Proxy base URL |
| `-key` | *(required)* | Virtual API key |
| `-admin-token` | *(uses -key)* | Admin token for model discovery |
| `-providers` | `Ollama Cloud,NanoGPT` | Comma-separated provider names to discover models from |
| `-users` | `8` | Concurrent simulated users |
| `-duration` | `10m` | Total run time |
| `-conv-min` | `1m` | Min conversation duration |
| `-conv-max` | `3m` | Max conversation duration |
| `-streaming` | `true` | Use streaming responses |
| `-max-tokens-min` | `10` | Min tokens per response (>=1) |
| `-max-tokens-max` | `500` | Max tokens per response (<=1000) |
| `-jitter` | `true` | Random 2-8s delay between turns |
| `-discover` | `true` | Auto-discover models from proxy API |
| `-pick` | `30` | Number of models to randomly pick from discovered pool |
| `-models` | *(discovery)* | Comma-separated `Provider/ModelID` list (overrides discovery) |

## Model Selection

By default, models are auto-discovered from the proxy API, filtered to text-only (excluding vision/embedding/speech), and restricted to the providers listed in `-providers`. A random subset of `-pick` models is chosen each run, so consecutive runs use different models.

Override with explicit model list:

```bash
go run . -key $KEY -models "NanoGPT/deepseek-chat,Ollama Cloud/gemma3:4b"
```

Use different providers:

```bash
go run . -key $KEY -providers "Ollama Cloud,NanoGPT,OpenRouter"
```

## Error Handling

- **429/502/503**: Model goes on 5-minute cooldown, user picks another
- **400/404/422**: Model marked dead permanently, never retried
- **All models unavailable**: User pauses 30s then retries

## Output

Status printed every 10 seconds (requests, errors, dead models, top 5 models by usage). Final stats on exit.
