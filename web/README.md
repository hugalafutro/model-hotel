# Model Hotel Frontend

React + TypeScript dashboard for the Model Hotel multi-provider AI gateway. Provides a web interface for managing LLM providers, virtual keys, monitoring usage, and interactive testing via chat and arena modes.

## Tech Stack

- **Framework**: React 18 with TypeScript
- **Build Tool**: Vite
- **Routing**: React Router v6
- **State Management**: React Context + TanStack Query
- **Styling**: Tailwind CSS with custom CSS variables
- **UI Components**: Custom component library
- **Icons**: Lucide React
- **API Client**: Fetch API withTanStack Query
- **Linting**: ESLint + TypeScript ESLint
- **Formatting**: Biome (configured for tabs)

## Project Structure

```
web/
├── public/                 # Static assets
├── dist/                   # Build output (served by Go backend)
├── src/
│   ├── api/               # API client and type definitions
│   │   ├── client.ts      # Fetch wrapper and auth
│   │   └── types.ts       # TypeScript interfaces
│   ├── components/        # Reusable UI components
│   │   ├── Layout.tsx     # Main layout with sidebar
│   │   ├── ModelPicker.tsx
│   │   ├── ModelReplyCard.tsx
│   │   ├── PersonaPicker.tsx
│   │   ├── PresetBar.tsx
│   │   └── ...           # 30+ components
│   ├── pages/            # Page components
│   │   ├── Dashboard.tsx # Overview and stats
│   │   ├── Providers.tsx # Provider management
│   │   ├── Models.tsx    # Model list and testing
│   │   ├── FailoverGroups.tsx
│   │   ├── VirtualKeys.tsx
│   │   ├── Logs.tsx      # Request logs
│   │   ├── AppLogs.tsx   # Application logs
│   │   ├── Settings.tsx  # Runtime configuration
│   │   ├── Chat.tsx      # Interactive chat
│   │   └── Arena.tsx     # Competition/compare modes
│   ├── context/          # React Context providers
│   │   ├── ThemeContext.tsx
│   │   ├── EventContext.tsx      # SSE events
│   │   ├── ToastContext.tsx
│   │   ├── StorageContext.tsx    # localStorage
│   │   └── SidebarModeContext.tsx
│   ├── data/             # Presets and static data
│   │   └── presets.ts    # Chat personas, arena prompts
│   ├── hooks/            # Custom React hooks
│   ├── utils/            # Utility functions
│   │   ├── arenaHistory.ts
│   │   ├── model.ts
│   │   ├── thinking.ts
│   │   └── stagger.ts
│   ├── App.tsx           # Root component
│   ├── main.tsx          # Entry point
│   └── index.css         # Global styles
├── index.html
├── package.json
├── tsconfig.json
├── vite.config.ts
└── biome.json
```

## Key Features

### Dashboard (`/dashboard`)
- Provider and model counts
- Recent request stats
- Quick actions (add provider, create key)

### Provider Management (`/providers`)
- Add/edit/delete LLM providers
- Auto-detect provider type from URL
- Manual and automatic model discovery
- View quota/balance (DeepSeek, NanoGPT, Z.AI)

### Model Management (`/models`)
- Browse discovered models
- Enable/disable models
- Test model connectivity
- View model capabilities and pricing

### Failover Groups (`/failover`)
- Configure `hotel/` routing groups
- Set provider priority order
- Enable/disable individual entries
- Sync groups with discovered models

### Virtual Keys (`/virtual-keys`)
- Create per-client API keys
- View usage statistics
- Revoke keys instantly

### Logs (`/logs`)
- **Request Logs**: Filterable request history with latency, tokens, errors
- **App Logs**: Application events and errors

### Settings (`/settings`)
- Runtime configuration UI
- Theme selection (dark/light)
- UI style presets (cyber-terminal, glassmorphism-lite)
- Accent color picker

### Chat (`/chat`)
Two sub-modes:
- **Chat**: Standard interactive chat with personas, streaming
- **Conversation**: Two models talking to each other (round-based)

### Arena (`/arena`)
Two sub-modes:
- **Competition**: Bracket tournaments with voting
- **Compare**: Side-by-side model comparison

## Development Setup

### Prerequisites
- Node.js 18+
- pnpm (recommended) or npm

### Install Dependencies
```bash
cd web
pnpm install
```

### Development Server
```bash
pnpm dev
```
Runs on <http://localhost:5173> (points to backend at localhost:8081 by default)

### Build for Production
```bash
pnpm build
```
Output goes to `web/dist/`, served by Go backend at `/`

## Configuration

The frontend reads from these sources:
1. **API_BASE**: Runtime config from `import.meta.env.VITE_API_BASE` or defaults to `/api`
2. **localStorage**: User preferences
   - `adminToken`: Authentication token
   - `theme`: dark/light
   - `accentColor`: Hex color
   - `uiStyle`: cyber-terminal, glassmorphism-lite, or default
   - `persistChat/persistConversation/persistArena`: State persistence flags

### Environment Variables
- `VITE_API_BASE`: Override API base URL (default: `/api`)
- `VITE_WS_BASE`: Override WebSocket base URL for SSE (default: same as API)

## State Management

### API Data (TanStack Query)
```typescript
// Example from Dashboard.tsx
const { data: stats } = useQuery({
  queryKey: ['stats'],
  queryFn: () => api.get('/stats'),
  refetchInterval: 10000, // Poll every 10s
})
```

### UI State (React Context)
```typescript
// Theme example
const { theme, accentColor, setTheme } = useTheme()

// Sidebar mode example
const { chatSubMode, setChatSubMode } = useSidebarMode()
```

### Persistent State (localStorage)
```typescript
// Automatically persisted via StorageContext
localStorage.setItem('adminToken', token)
localStorage.setItem('persistChat', 'true')
```

## API Integration

All API calls go through `src/api/client.ts`:
- Automatic admin token injection
- JSON parsing/serialization
- Error handling
- Type-safe responses

```typescript
import { api } from '../api/client'

// Typed response
const providers = await api.get<Provider[]>('/providers')
```

## Styling

### Tailwind CSS with Custom Properties
```css
/* index.css */
@theme {
  --color-accent: var(--accent-color, #1dd1a1);
  --font-mono: 'JetBrains Mono', 'SFMono-Regular', monospace;
}
```

### Theme Variables
- `--accent-color`: Primary brand color
- `--bg-primary`: Main background
- `--bg-secondary`: Panel background
- `--text-primary`: Primary text color
- `--text-secondary`: Secondary text color

### Dark/Light Mode
Uses CSS variables that switch based on `data-theme` attribute:
```css
[data-theme="dark"] { /* dark mode variables */ }
[data-theme="light"] { /* light mode variables */ }
```

### UI Style Presets
- **default**: Standard dark/light theme
- **cyber-terminal**: Green on black monospace
- **glassmorphism-lite**: Semi-transparent panels

## SSE Events

Real-time events via Server-Sent Events:
```typescript
// src/context/EventContext.tsx
const eventSource = new EventSource('/api/events', {
  headers: { Authorization: `Bearer ${adminToken}` }
})

eventSource.addEventListener('discovery.finished', (e) => {
  const data = JSON.parse(e.data)
  showToast(`${data.models_discovered} models discovered`)
})
```

Event types:
- `discovery.started/finished`
- `discovery.provider_error`
- `discovery.models_disabled`
- `failover.sync_error`
- `model.disabled_manually`

## Testing

### Run Tests
```bash
# Unit tests
pnpm test

# Linting
pnpm lint

# Type checking
pnpm typecheck
```

### Manual Testing
1. Start backend: `docker compose -f docker-compose.yml -f compose.dev.yml up` (or `go run cmd/server/main.go`)
2. Start frontend: `pnpm dev`
3. Login with admin token
4. Add a provider and test

## Performance Considerations

### Optimizations
- Lazy loaded routes (React.lazy + Suspense)
- Virtualized lists for large datasets (logs, models)
- Debounced filters and search
- Optimistic updates where appropriate
- Inline SVG icons (no icon font)

### Bundle Size
- Tree-shaken imports
- Code splitting by route
- Vendor chunk separation (Vite default)

### Caching
- TanStack Query handles API response caching
- Model/state lists refetch intelligently
- Stale-while-revalidate pattern

## Common Development Tasks

### Adding a New Page
1. Create `src/pages/NewPage.tsx`
2. Add route in `src/App.tsx`
3. Add navigation in `src/components/Layout.tsx`
4. Create API types in `src/api/types.ts`

### Adding a New Component
1. Create `src/components/Component.tsx`
2. Use TypeScript interfaces for props
3. Follow existing style patterns
4. Add to storybook (if applicable)

### Modifying API Calls
1. Update types in `src/api/types.ts`
2. Modify call in component or add to `src/api/client.ts`
3. Handle loading/error states

### Changing Styles
1. Update Tailwind classes or CSS variables
2. Consider dark/light mode
3. Test with different UI style presets

## Build & Deployment

### Production Build
```bash
pnpm build
```
Creates optimized bundle in `web/dist/`

### Docker Deployment
The Go backend serves the built frontend:
```go
// cmd/server/static.go
r.Get("/*", spaHandler.ServeHTTP)
```

### Environment-Specific Builds
```bash
# Development
VITE_API_BASE=http://localhost:8081 pnpm dev

# Production (default)
pnpm build
```

## Troubleshooting

### CORS Issues
- Ensure backend CORS_ORIGINS includes frontend origin
- Check browser console for errors
- Verify admin token is set

### API Errors
- Check browser network tab
- Verify backend is running
- Check backend logs: `docker compose -f docker-compose.yml -f compose.dev.yml logs app`

### Build Failures
- Clear node_modules and reinstall
- Check TypeScript errors: `pnpm typecheck`
- Verify Vite config

### Hot Reload Not Working
- Ensure WDS port not blocked
- Check browser console for WS errors
- Restart dev server

## Contributing

When contributing to the frontend:

1. Follow TypeScript strict mode
2. Use Biome formatting (tabs)
3. Add tests for new features
4. Update this README if adding major features
5. Consider accessibility (ARIA labels, keyboard nav)
6. Test with both dark and light themes
7. Verify mobile responsiveness

## Resources

- [Main Project README](../README.md)
- [API Reference](https://github.com/hugalafutro/model-hotel/wiki/API-Reference)
- [Configuration](https://github.com/hugalafutro/model-hotel/wiki/Configuration)
```
