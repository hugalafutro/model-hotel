import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useTheme } from '../context/ThemeContext'
import { useToast } from '../context/ToastContext'

const DISCOVERY_INTERVALS = [
  { value: '30m', label: '30 minutes' },
  { value: '1h', label: '1 hour' },
  { value: '6h', label: '6 hours' },
  { value: '12h', label: '12 hours' },
  { value: '24h', label: '24 hours' },
  { value: '0', label: 'Disabled' },
]

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  const diffDay = Math.floor(diffHr / 24)
  return `${diffDay}d ago`
}

export function Settings() {
  const { theme, setTheme } = useTheme()
  const { toast } = useToast()
  const queryClient = useQueryClient()

  const { data: settings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.settings.get(),
  })

  const updateMutation = useMutation({
    mutationFn: (updates: Record<string, string>) => api.settings.update(updates),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      toast('Settings saved', 'success')
    },
    onError: (err: Error) => {
      toast(`Failed to save: ${err.message}`, 'error')
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-indigo-400"></div>
      </div>
    )
  }

  const discoveryInterval = settings?.discovery_interval || '6h'
  const discoveryOnStartup = settings?.discovery_on_startup !== 'false'
  const discoveryOnCreate = settings?.discovery_on_provider_create !== 'false'

  return (
    <div className="space-y-8 max-w-2xl">
      <div>
        <h1 className="text-3xl font-bold text-white">Settings</h1>
        <p className="text-gray-400 mt-1">Configure your LLM-Proxy instance</p>
      </div>

      {/* Model Discovery */}
      <div className="bg-gray-800 border border-gray-700 rounded-xl p-6">
        <h2 className="text-xl font-semibold text-white mb-4">Model Discovery</h2>
        <p className="text-gray-400 text-sm mb-6">
          Configure how and when models are auto-discovered from your providers.
        </p>

        <div className="space-y-5">
          <div>
            <label htmlFor="discovery-interval" className="block text-sm font-medium text-gray-300 mb-2">
              Discovery Interval
            </label>
            <select
              id="discovery-interval"
              value={discoveryInterval}
              onChange={(e) => updateMutation.mutate({ discovery_interval: e.target.value })}
              className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
            >
              {DISCOVERY_INTERVALS.map(opt => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
            <p className="text-gray-500 text-xs mt-1">How often to automatically re-discover models from all enabled providers</p>
          </div>

          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-300">Discover on Startup</p>
              <p className="text-gray-500 text-xs mt-0.5">Run discovery for all providers when the server starts</p>
            </div>
            <button
              type="button"
              onClick={() => updateMutation.mutate({ discovery_on_startup: discoveryOnStartup ? 'false' : 'true' })}
              className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                discoveryOnStartup ? 'bg-indigo-500' : 'bg-gray-600'
              }`}
            >
              <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                discoveryOnStartup ? 'translate-x-6' : 'translate-x-1'
              }`} />
            </button>
          </div>

          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-300">Discover on Provider Creation</p>
              <p className="text-gray-500 text-xs mt-0.5">Automatically discover models when a new provider is added</p>
            </div>
            <button
              type="button"
              onClick={() => updateMutation.mutate({ discovery_on_provider_create: discoveryOnCreate ? 'false' : 'true' })}
              className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                discoveryOnCreate ? 'bg-indigo-500' : 'bg-gray-600'
              }`}
            >
              <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                discoveryOnCreate ? 'translate-x-6' : 'translate-x-1'
              }`} />
            </button>
          </div>
        </div>
      </div>

      {/* Theme */}
      <div className="bg-gray-800 border border-gray-700 rounded-xl p-6">
        <h2 className="text-xl font-semibold text-white mb-4">Appearance</h2>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium text-gray-300">Theme</p>
            <p className="text-gray-500 text-xs mt-0.5">Switch between dark and light mode</p>
          </div>
          <div className="flex rounded-lg overflow-hidden border border-gray-600">
            <button
              type="button"
              onClick={() => setTheme('dark')}
              className={`px-4 py-2 text-sm font-medium transition-colors ${
                theme === 'dark' ? 'bg-indigo-500 text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
              }`}
            >
              Dark
            </button>
            <button
              type="button"
              onClick={() => setTheme('light')}
              className={`px-4 py-2 text-sm font-medium transition-colors ${
                theme === 'light' ? 'bg-indigo-500 text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
              }`}
            >
              Light
            </button>
          </div>
        </div>
      </div>

      {/* Provider List with Discovery Status */}
      <div className="bg-gray-800 border border-gray-700 rounded-xl p-6">
        <h2 className="text-xl font-semibold text-white mb-4">Provider Discovery Status</h2>
        <ProviderDiscoveryList />
      </div>

      </div>
  )
}

function ProviderDiscoveryList() {
  const { toast } = useToast()
  const { data: providers, isLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const { data: models } = useQuery({
    queryKey: ['models'],
    queryFn: () => api.models.list(),
  })

  const queryClient = useQueryClient()

  const discoverMutation = useMutation({
    mutationFn: async (id: string) => {
      toast('Discovering models...', 'info')
      return api.providers.discover(id)
    },
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      queryClient.invalidateQueries({ queryKey: ['models'] })
      toast(`Discovered ${data?.discovered ?? 'new'} models`, 'success')
    },
    onError: (err: Error) => {
      toast(`Discovery failed: ${err.message}`, 'error')
    },
  })

  if (isLoading) return <p className="text-gray-500">Loading providers...</p>

  const modelCounts: Record<string, number> = {}
  for (const m of models || []) {
    modelCounts[m.provider_id] = (modelCounts[m.provider_id] || 0) + 1
  }

  return (
    <div className="space-y-3">
      {providers?.length === 0 && (
        <p className="text-gray-500 text-sm">No providers configured yet.</p>
      )}
      {providers?.map(p => (
        <div key={p.id} className="flex items-center justify-between py-2 border-b border-gray-700 last:border-0">
          <div className="flex items-center gap-3">
            <span className={`w-2 h-2 rounded-full ${p.enabled ? 'bg-green-400' : 'bg-gray-500'}`} />
            <div>
              <p className="text-sm font-medium text-white">{p.name}</p>
              <p className="text-xs text-gray-500">
                {modelCounts[p.id] || 0} models
                {p.last_discovered_at && ` · Last discovered ${formatRelativeTime(p.last_discovered_at)}`}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={() => discoverMutation.mutate(p.id)}
            disabled={discoverMutation.isPending}
            className="px-3 py-1 text-sm text-indigo-400 hover:bg-blue-900/30 rounded transition-colors disabled:opacity-50"
          >
            Discover Now
          </button>
        </div>
      ))}
    </div>
  )
}