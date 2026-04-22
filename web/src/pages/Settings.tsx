import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useTheme } from '../context/ThemeContext'
import { useToast } from '../context/ToastContext'
import { useState } from 'react'

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
  const { theme, setTheme, accentColor, setAccentColor, accentPresets } = useTheme()
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
        <div className="animate-spin rounded-full h-12 w-12 border-b-2" style={{ borderColor: 'var(--accent)' }}></div>
      </div>
    )
  }

  const discoveryInterval = settings?.discovery_interval || '6h'
  const discoveryOnStartup = settings?.discovery_on_startup !== 'false'
  const discoveryOnCreate = settings?.discovery_on_provider_create !== 'false'

  return (
    <div className="space-y-8 max-w-5xl">
      <div>
        <h1 className="text-3xl font-bold text-white">Settings</h1>
        <p className="text-gray-400 mt-1">Configure your LLM-Proxy instance</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
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
                className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:ring-2 focus:ring-[var(--accent)] focus:border-transparent outline-none"
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
                  discoveryOnStartup ? 'bg-[var(--accent)]' : 'bg-gray-600'
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
                  discoveryOnCreate ? 'bg-[var(--accent)]' : 'bg-gray-600'
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
          <div className="space-y-5">
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
                    theme === 'dark' ? 'bg-[var(--accent)] text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
                  }`}
                >
                  Dark
                </button>
                <button
                  type="button"
                  onClick={() => setTheme('light')}
                  className={`px-4 py-2 text-sm font-medium transition-colors ${
                    theme === 'light' ? 'bg-[var(--accent)] text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
                  }`}
                >
                  Light
                </button>
              </div>
            </div>

            <div>
              <p className="text-sm font-medium text-gray-300 mb-2">Accent Color</p>
              <div className="flex flex-wrap gap-2">
                {accentPresets.map(preset => (
                  <button
                    key={preset.name}
                    type="button"
                    onClick={() => setAccentColor(preset.color)}
                    className={`w-8 h-8 rounded-full border-2 transition-transform hover:scale-110 ${
                      accentColor === preset.color ? 'border-white scale-110' : 'border-transparent'
                    }`}
                    style={{ backgroundColor: preset.color }}
                    title={preset.name}
                  />
                ))}
                <label className="relative cursor-pointer">
                  <input
                    type="color"
                    value={accentColor}
                    onChange={(e) => setAccentColor(e.target.value)}
                    className="absolute inset-0 opacity-0 w-8 h-8"
                  />
                  <div 
                    className="w-8 h-8 rounded-full border-2 border-dashed border-gray-500 flex items-center justify-center hover:border-gray-400 transition-colors"
                    title="Custom color"
                  >
                    <svg className="w-4 h-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                    </svg>
                  </div>
                </label>
              </div>
            </div>
          </div>
        </div>

        {/* Logging */}
        <LoggingSettings />

        {/* Provider List with Discovery Status */}
        <div className="bg-gray-800 border border-gray-700 rounded-xl p-6">
          <h2 className="text-xl font-semibold text-white mb-4">Provider Discovery Status</h2>
          <ProviderDiscoveryList />
        </div>
      </div>

      </div>
  )
}

const LOG_RETENTION_OPTIONS = [
  { value: '0', label: 'Disabled' },
  { value: '24h', label: '1 day' },
  { value: '168h', label: '1 week' },
  { value: '720h', label: '1 month' },
]

function LoggingSettings() {
  const { toast } = useToast()
  const queryClient = useQueryClient()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleteSelection, setDeleteSelection] = useState('')

  const { data: settings } = useQuery({
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

  const purgeMutation = useMutation({
    mutationFn: (olderThan: string) => api.logs.purge(olderThan),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['logs'] })
      toast('Logs deleted', 'success')
      setConfirmDelete(false)
      setDeleteSelection('')
    },
    onError: (err: Error) => {
      toast(`Failed to delete logs: ${err.message}`, 'error')
      setConfirmDelete(false)
    },
  })

  const logRetention = settings?.log_retention || '0'

  const getDeleteOlderThan = (selection: string): string => {
    switch (selection) {
      case '1d': return '24h'
      case '1w': return '168h'
      case '1m': return '720h'
      case 'all': return 'all'
      default: return ''
    }
  }

  return (
    <div className="bg-gray-800 border border-gray-700 rounded-xl p-6">
      <h2 className="text-xl font-semibold text-white mb-4">Logging</h2>

      <div className="space-y-5">
        <div>
          <label htmlFor="log-retention" className="block text-sm font-medium text-gray-300 mb-2">
            Log Retention
          </label>
          <select
            id="log-retention"
            value={logRetention}
            onChange={(e) => updateMutation.mutate({ log_retention: e.target.value })}
            className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:ring-2 focus:ring-[var(--accent)] focus:border-transparent outline-none"
          >
            {LOG_RETENTION_OPTIONS.map(opt => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
          <p className="text-gray-500 text-xs mt-1">Automatically delete logs older than this period</p>
        </div>

        <div>
          {!confirmDelete ? (
            <button
              type="button"
              onClick={() => setConfirmDelete(true)}
              className="px-3 py-1.5 text-xs rounded-full border bg-red-900/40 text-red-300 border-red-700/50 cursor-pointer hover:brightness-125 transition-all"
            >
              Delete Logs
            </button>
          ) : (
            <div className="space-y-3">
              <div className="flex items-center gap-2">
                <select
                  value={deleteSelection}
                  onChange={(e) => setDeleteSelection(e.target.value)}
                  className="px-3 py-1.5 bg-gray-700 border border-gray-600 rounded-lg text-white text-xs focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
                >
                  <option value="">Select range...</option>
                  <option value="1d">Older than 1 day</option>
                  <option value="1w">Older than 1 week</option>
                  <option value="1m">Older than 1 month</option>
                  <option value="all">All logs</option>
                </select>
                <button
                  type="button"
                  disabled={!deleteSelection}
                  onClick={() => {
                    const olderThan = getDeleteOlderThan(deleteSelection)
                    if (olderThan) purgeMutation.mutate(olderThan)
                  }}
                  className="px-3 py-1.5 text-xs rounded-full border bg-red-600 text-white border-red-500 cursor-pointer hover:bg-red-500 disabled:opacity-50 disabled:cursor-not-allowed transition-all"
                >
                  Confirm Delete
                </button>
                <button
                  type="button"
                  onClick={() => { setConfirmDelete(false); setDeleteSelection('') }}
                  className="px-3 py-1.5 text-xs rounded-full border bg-gray-700 text-gray-300 border-gray-600 cursor-pointer hover:bg-gray-600 transition-all"
                >
                  Cancel
                </button>
              </div>
            </div>
          )}
        </div>
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
  const [discoveringId, setDiscoveringId] = useState<string | null>(null)

  const discoverMutation = useMutation({
    mutationFn: async (id: string) => {
      setDiscoveringId(id)
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
    onSettled: () => {
      setDiscoveringId(null)
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
        <div key={p.id} className="flex items-center justify-between py-2">
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
            disabled={discoveringId !== null}
            className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
              discoveringId === p.id
                ? 'bg-[var(--accent-lighter)] text-[var(--accent)] border-[var(--accent-light)] cursor-not-allowed'
                : discoveringId !== null
                ? 'bg-gray-800/50 text-gray-600 border-gray-700/30 cursor-not-allowed'
                : 'bg-[var(--accent-light)] text-[var(--accent)] border-[var(--accent-lighter)] cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(129,140,248,0.2)]'
            }`}
          >
            {discoveringId === p.id ? 'Discovering...' : 'Discover Now'}
          </button>
        </div>
      ))}
    </div>
  )
}