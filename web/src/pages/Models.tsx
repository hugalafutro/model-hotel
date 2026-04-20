import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState, useMemo, useCallback, useEffect, useRef } from 'react'
import type { Model, ModelCapabilities } from '../api/types'
import { useToast } from '../context/ToastContext'

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

function formatNumber(n: number | null | undefined): string {
  if (n == null) return '-'
  return n.toLocaleString()
}

function parseCapabilities(raw: string): ModelCapabilities | null {
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

function parseParams(raw: string): Record<string, unknown> | null {
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

type CapKey = 'vision' | 'reasoning' | 'tool_calling' | 'structured_output' | 'pdf_upload' | 'video_input' | 'audio_input' | 'parallel_tool_calls'

const CAP_META: { key: CapKey; label: string; style: string; muted: string; disabled: string }[] = [
  { key: 'vision', label: 'Vision', style: 'bg-purple-900/40 text-purple-300 border-purple-700/50 shadow-[0_0_6px_1px_rgba(147,51,234,0.35)]', muted: 'bg-purple-900/15 text-purple-500/60 border-purple-700/25 hover:bg-purple-900/25 hover:text-purple-400', disabled: 'bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50' },
  { key: 'reasoning', label: 'Reasoning', style: 'bg-amber-900/40 text-amber-300 border-amber-700/50 shadow-[0_0_6px_1px_rgba(245,158,11,0.35)]', muted: 'bg-amber-900/15 text-amber-500/60 border-amber-700/25 hover:bg-amber-900/25 hover:text-amber-400', disabled: 'bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50' },
  { key: 'tool_calling', label: 'Tools', style: 'bg-cyan-900/40 text-cyan-300 border-cyan-700/50 shadow-[0_0_6px_1px_rgba(6,182,212,0.35)]', muted: 'bg-cyan-900/15 text-cyan-500/60 border-cyan-700/25 hover:bg-cyan-900/25 hover:text-cyan-400', disabled: 'bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50' },
  { key: 'structured_output', label: 'Structured', style: 'bg-emerald-900/40 text-emerald-300 border-emerald-700/50 shadow-[0_0_6px_1px_rgba(16,185,129,0.35)]', muted: 'bg-emerald-900/15 text-emerald-500/60 border-emerald-700/25 hover:bg-emerald-900/25 hover:text-emerald-400', disabled: 'bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50' },
  { key: 'pdf_upload', label: 'PDF', style: 'bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]', muted: 'bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400', disabled: 'bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50' },
  { key: 'video_input', label: 'Video', style: 'bg-pink-900/40 text-pink-300 border-pink-700/50 shadow-[0_0_6px_1px_rgba(236,72,153,0.35)]', muted: 'bg-pink-900/15 text-pink-500/60 border-pink-700/25 hover:bg-pink-900/25 hover:text-pink-400', disabled: 'bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50' },
  { key: 'audio_input', label: 'Audio', style: 'bg-orange-900/40 text-orange-300 border-orange-700/50 shadow-[0_0_6px_1px_rgba(249,115,22,0.35)]', muted: 'bg-orange-900/15 text-orange-500/60 border-orange-700/25 hover:bg-orange-900/25 hover:text-orange-400', disabled: 'bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50' },
  { key: 'parallel_tool_calls', label: 'Parallel', style: 'bg-teal-900/40 text-teal-300 border-teal-700/50 shadow-[0_0_6px_1px_rgba(20,184,166,0.35)]', muted: 'bg-teal-900/15 text-teal-500/60 border-teal-700/25 hover:bg-teal-900/25 hover:text-teal-400', disabled: 'bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50' },
]

function hasCap(caps: ModelCapabilities | null, key: CapKey): boolean {
  if (!caps) return false
  return !!caps[key]
}

function CapBadge({ caps, capKey }: { caps: ModelCapabilities | null; capKey: CapKey }) {
  const meta = CAP_META.find(m => m.key === capKey)
  if (!meta || !hasCap(caps, capKey)) return null
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-medium border mr-1 ${meta.style}`}>
      {meta.label}
    </span>
  )
}

type SortField = 'name' | 'provider' | 'discovered' | 'status'
type SortDir = 'asc' | 'desc'
type StatusFilter = 'enabled' | 'disabled'

function SortHeader({ label, field, sort, onSort }: { label: string; field: SortField; sort: { field: SortField; dir: SortDir }; onSort: (f: SortField) => void }) {
  const active = sort.field === field
  return (
    <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider cursor-pointer select-none hover:text-gray-200 text-gray-400 whitespace-nowrap" onClick={() => onSort(field)}>
      {label} <span className="inline-block w-3 text-center">{active ? (sort.dir === 'asc' ? '↑' : '↓') : ' '}</span>
    </th>
  )
}

function ModelDetailModal({ model, onClose, onToggle, onDiscover }: { model: Model; onClose: () => void; onToggle: (id: string, enabled: boolean) => void; onDiscover: (providerId: string) => Promise<unknown> }) {
  const caps = parseCapabilities(model.capabilities)
  const params = parseParams(model.params)
  const inputMods = (() => { try { return JSON.parse(model.input_modalities) as string[] } catch { return [] } })()
  const outputMods = (() => { try { return JSON.parse(model.output_modalities) as string[] } catch { return [] } })()
  const [cooldown, setCooldown] = useState(0)
  const [discovering, setDiscovering] = useState(false)
  const timerRef = useRef<ReturnType<typeof setInterval>>()

  useEffect(() => {
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [])

  const handleDiscover = async () => {
    if (cooldown > 0 || discovering) return
    setDiscovering(true)
    try {
      await onDiscover(model.provider_id)
      setCooldown(30)
      timerRef.current = setInterval(() => {
        setCooldown(prev => {
          if (prev <= 1) {
            if (timerRef.current) clearInterval(timerRef.current)
            return 0
          }
          return prev - 1
        })
      }, 1000)
    } finally {
      setDiscovering(false)
    }
  }

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 flex items-center justify-center z-50" onKeyDown={(e) => { if (e.key === 'Escape') onClose() }}>
      <button type="button" className="absolute inset-0 bg-black/60 cursor-default" onClick={onClose} aria-label="Close dialog" />
      <div className="relative bg-gray-800 border border-gray-700 rounded-2xl p-6 w-full max-w-lg max-h-[85vh] overflow-y-auto">
        <div className="flex justify-between items-start mb-4">
          <div>
            <h2 className="text-xl font-bold text-white">{model.name || model.model_id}</h2>
            <p className="text-sm text-gray-400 mt-1 font-mono">{model.model_id}</p>
          </div>
          <button type="button" onClick={onClose} className="text-gray-400 hover:text-white text-xl leading-none" aria-label="Close">&times;</button>
        </div>

        {model.description && (
          <p className="text-sm text-gray-300 mb-4">{model.description}</p>
        )}

        <div className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm mb-4">
          <div>
            <span className="text-gray-500">Provider</span>
            <p className="text-gray-200">{model.provider_name}</p>
          </div>
          <div>
            <span className="text-gray-500">Last Discovered</span>
            <p className="text-gray-200">{formatRelativeTime(model.last_seen_at)}</p>
          </div>
          <div>
            <span className="text-gray-500">Context Length</span>
            <p className="text-gray-200">{formatNumber(model.context_length)} tokens</p>
          </div>
          <div>
            <span className="text-gray-500">Max Output</span>
            <p className="text-gray-200">{formatNumber(model.max_output_tokens)} tokens</p>
          </div>
          <div>
            <span className="text-gray-500">Input</span>
            <p className="text-gray-200">{inputMods.join(', ') || 'text'}</p>
          </div>
          <div>
            <span className="text-gray-500">Output</span>
            <p className="text-gray-200">{outputMods.join(', ') || 'text'}</p>
          </div>
        </div>

        {caps && (
          <div className="mb-4">
            <h3 className="text-sm font-medium text-gray-400 mb-2">Capabilities</h3>
            <div className="flex flex-wrap gap-1">
              {CAP_META.map(m => (
                <CapBadge key={m.key} caps={caps} capKey={m.key} />
              ))}
            </div>
            {!CAP_META.some(m => hasCap(caps, m.key)) && (
              <p className="text-sm text-gray-500">No special capabilities detected</p>
            )}
          </div>
        )}

        {params && params.subscription_included !== undefined && (
          <div className="mb-4">
            <h3 className="text-sm font-medium text-gray-400 mb-2">Subscription</h3>
            <div className="flex items-center gap-2">
              <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
                params.subscription_included ? 'bg-green-900/40 text-green-300 border border-green-700/50' : 'bg-yellow-900/40 text-yellow-300 border border-yellow-700/50'
              }`}>
                {params.subscription_included ? 'Included' : 'Not included'}
              </span>
              {params.subscription_note ? (
                <span className="text-sm text-gray-500">{String(params.subscription_note)}</span>
              ) : null}
            </div>
          </div>
        )}

        <div className="flex items-center justify-between mt-4 pt-4 border-t border-gray-700">
          <button
            type="button"
            onClick={() => onToggle(model.id, !model.enabled)}
            className={`px-3 py-1.5 text-xs rounded-full border cursor-pointer transition-all ${
              model.enabled
                ? 'bg-green-900/50 text-green-400 border-green-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(34,197,94,0.2)]'
                : 'bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)]'
            }`}
          >
            {model.enabled ? 'Enabled (click to disable)' : 'Disabled (click to enable)'}
          </button>
          <button
            type="button"
            disabled={cooldown > 0 || discovering}
            onClick={handleDiscover}
            className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
              cooldown > 0 || discovering
                ? 'bg-blue-900/20 text-blue-500/50 border-blue-700/20 cursor-not-allowed'
                : 'bg-blue-900/40 text-blue-300 border-blue-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(59,130,246,0.2)]'
            }`}
          >
            {discovering ? 'Updating...' : cooldown > 0 ? `Update info (${cooldown}s)` : 'Update info'}
          </button>
        </div>
      </div>
    </div>
  )
}

function matchesAllCaps(caps: ModelCapabilities | null, keys: Set<CapKey>): boolean {
  if (keys.size === 0) return true
  for (const k of keys) {
    if (!hasCap(caps, k)) return false
  }
  return true
}

export function Models() {
  const { toast } = useToast()
  const queryClient = useQueryClient()
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedProvider, setSelectedProvider] = useState<string>('')
  const [detailModel, setDetailModel] = useState<Model | null>(null)
  const [sort, setSort] = useState<{ field: SortField; dir: SortDir }>({ field: 'name', dir: 'asc' })
  const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set())
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('enabled')

  const { data: models, isLoading } = useQuery({
    queryKey: ['models', selectedProvider],
    queryFn: () => api.models.list(selectedProvider || undefined),
  })

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const toggleMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.models.update(id, { enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['models'] })
    },
    onError: (err: Error) => {
      toast(`Failed to update model: ${err.message}`, 'error')
    },
  })

  const handleToggleModel = useCallback((id: string, enabled: boolean) => {
    toggleMutation.mutate({ id, enabled }, {
      onSuccess: () => {
        toast(enabled ? 'Model enabled' : 'Model disabled', enabled ? 'success' : 'error')
        setDetailModel(prev => prev ? { ...prev, enabled } : null)
      },
    })
  }, [toggleMutation, toast])

  const handleDiscover = useCallback(async (providerId: string) => {
    toast('Discovering models...', 'info')
    const result = await api.providers.discover(providerId)
    queryClient.invalidateQueries({ queryKey: ['models'] })
    queryClient.invalidateQueries({ queryKey: ['providers'] })
    toast(`Discovered ${result?.discovered ?? 'new'} models`, 'success')
    return result
  }, [queryClient, toast])

  const copyModelId = useCallback((modelId: string) => {
    navigator.clipboard.writeText(modelId).then(() => {
      toast(`Copied: ${modelId}`, 'info')
    }).catch(() => {
      toast('Failed to copy', 'error')
    })
  }, [toast])

  const toggleCapFilter = useCallback((key: CapKey) => {
    setCapFilter(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }, [])

  const handleSort = (field: SortField) => {
    setSort(prev => ({
      field,
      dir: prev.field === field && prev.dir === 'asc' ? 'desc' : 'asc',
    }))
  }

  const { sortedAndFiltered, pillAvailability, existingCaps } = useMemo(() => {
    const baseFiltered = models?.filter((model) =>
      model.model_id.toLowerCase().includes(searchQuery.toLowerCase()) ||
      model.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
      model.display_name?.toLowerCase().includes(searchQuery.toLowerCase())
    ) || []

    const capsInData = new Set<CapKey>()
    for (const m of baseFiltered) {
      const c = parseCapabilities(m.capabilities)
      for (const meta of CAP_META) {
        if (hasCap(c, meta.key)) capsInData.add(meta.key)
      }
    }

    let filtered = baseFiltered

    if (capFilter.size > 0) {
      filtered = filtered.filter(m => matchesAllCaps(parseCapabilities(m.capabilities), capFilter))
    }

    filtered = filtered.filter(m => statusFilter === 'enabled' ? m.enabled : !m.enabled)

    const availability = new Map<CapKey, boolean>()
    for (const m of CAP_META) {
      const testFilter = new Set(capFilter)
      testFilter.add(m.key)
      const hasMatch = baseFiltered.some(model => matchesAllCaps(parseCapabilities(model.capabilities), testFilter))
      availability.set(m.key, hasMatch)
    }

    const dir = sort.dir === 'asc' ? 1 : -1
    filtered.sort((a, b) => {
      switch (sort.field) {
        case 'name': return dir * (a.name || a.model_id).localeCompare(b.name || b.model_id)
        case 'provider': return dir * a.provider_name.localeCompare(b.provider_name)
        case 'discovered': return dir * (new Date(a.last_seen_at).getTime() - new Date(b.last_seen_at).getTime())
        case 'status': return dir * (a.enabled === b.enabled ? 0 : a.enabled ? -1 : 1)
        default: return 0
      }
    })

    return { sortedAndFiltered: filtered, pillAvailability: availability, existingCaps: capsInData }
  }, [models, searchQuery, sort, capFilter, statusFilter])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500"></div>
      </div>
    )
  }

  const totalEnabled = models?.filter(m => m.enabled).length ?? 0
  const totalDisabled = (models?.length ?? 0) - totalEnabled
  const allSameState = totalEnabled === 0 || totalDisabled === 0

  return (
    <div className="space-y-4">
      <div>
        <div className="flex items-center gap-3">
          <h1 className="text-3xl font-bold text-white">{models?.length ?? 0} Models</h1>
          {!allSameState && (
            <span className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-700/60 border border-gray-600/50">
              <span className="text-green-400">{totalEnabled} enabled</span>
              <span className="text-gray-600">/</span>
              <span className="text-red-400">{totalDisabled} disabled</span>
            </span>
          )}
        </div>
        <p className="text-gray-400 mt-1">Discovered LLM models from your providers</p>
      </div>

      <div className="flex flex-col md:flex-row gap-4">
        <div className="flex-1">
          <input
            type="text"
            placeholder="Search models..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
          />
        </div>
        <div className="md:w-64">
          <select
            value={selectedProvider}
            onChange={(e) => setSelectedProvider(e.target.value)}
            className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
          >
            <option value="">All Providers</option>
            {providers?.map((provider) => (
              <option key={provider.id} value={provider.id}>
                {provider.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="bg-gray-800 border border-gray-700 rounded-xl overflow-hidden">
        <table className="min-w-full table-fixed divide-y divide-gray-700">
          <colgroup>
            <col className="w-[28%]" />
            <col className="w-[32%]" />
            <col className="w-[15%]" />
            <col className="w-[13%]" />
            <col className="w-[12%]" />
          </colgroup>
          <thead>
            <tr>
              <SortHeader label="Model" field="name" sort={sort} onSort={handleSort} />
              <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider text-gray-400">
                <span className="inline-flex items-center gap-1.5 flex-wrap">
                  Capabilities
                  {CAP_META.filter(m => existingCaps.has(m.key)).map(m => {
                    const isActive = capFilter.has(m.key)
                    const isAvailable = pillAvailability.get(m.key) ?? false
                    const isDisabled = !isActive && !isAvailable
                    return (
                      <button
                        key={m.key}
                        type="button"
                        disabled={isDisabled}
                        onClick={() => toggleCapFilter(m.key)}
                        className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${isActive ? m.style : isDisabled ? m.disabled : m.muted}`}
                      >
                        {m.label}
                      </button>
                    )
                  })}
                  {capFilter.size > 0 && (
                    <button
                      type="button"
                      onClick={() => setCapFilter(new Set())}
                      className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-gray-400 hover:text-gray-200"
                    >
                      ✕
                    </button>
                  )}
                </span>
              </th>
              <SortHeader label="Provider" field="provider" sort={sort} onSort={handleSort} />
              <SortHeader label="Discovered" field="discovered" sort={sort} onSort={handleSort} />
              <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider text-gray-400">
                <span className="inline-flex items-center gap-1.5">
                  Status
                  <button
                    type="button"
                    onClick={() => setStatusFilter('enabled')}
                    className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${
                      statusFilter === 'enabled'
                        ? 'bg-green-900/40 text-green-300 border-green-700/50 shadow-[0_0_6px_1px_rgba(34,197,94,0.35)]'
                        : 'bg-green-900/15 text-green-500/60 border-green-700/25 hover:bg-green-900/25 hover:text-green-400'
                    }`}
                  >
                    Enabled
                  </button>
                  <button
                    type="button"
                    onClick={() => setStatusFilter('disabled')}
                    className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${
                      statusFilter === 'disabled'
                        ? 'bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]'
                        : 'bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400'
                    }`}
                  >
                    Disabled
                  </button>
                </span>
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-700/50">
            {sortedAndFiltered.length > 0 ? (
              sortedAndFiltered.map((model) => {
                const caps = parseCapabilities(model.capabilities)
                return (
                  <tr key={model.id} className="hover:bg-gray-750">
                    <td className="px-4 py-1.5">
                      <div className="flex flex-col">
                        <button type="button" onClick={() => setDetailModel(model)} className="text-left hover:underline text-sm font-medium text-blue-400">
                          {model.name || model.model_id}
                        </button>
                        <button
                          type="button"
                          className="text-left text-[11px] text-gray-500 font-mono leading-tight cursor-pointer hover:text-gray-300 transition-colors"
                          onClick={() => copyModelId(model.model_id)}
                          title="Click to copy model ID"
                        >
                          {model.model_id}
                        </button>
                      </div>
                    </td>
                    <td className="px-4 py-1.5">
                      <div className="flex flex-wrap">
                        {CAP_META.map(m => (
                          <CapBadge key={m.key} caps={caps} capKey={m.key} />
                        ))}
                      </div>
                    </td>
                    <td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">{model.provider_name}</td>
                    <td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-400">{formatRelativeTime(model.last_seen_at)}</td>
                    <td className="px-4 py-1.5 whitespace-nowrap">
                      <span className={`px-2 py-0.5 text-xs rounded-full ${model.enabled ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}`}>
                        {model.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </td>
                  </tr>
                )
              })
            ) : (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-gray-500">
                  {searchQuery || selectedProvider || capFilter.size > 0
                    ? 'No models match your filters'
                    : 'No models discovered yet. Add a provider and discover models.'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {models && models.length > 0 && (
        <div className="text-sm text-gray-500 text-center">
          Showing {sortedAndFiltered.length} of {models.length} models
        </div>
      )}

      {detailModel && (
        <ModelDetailModal
          model={detailModel}
          onClose={() => setDetailModel(null)}
          onToggle={handleToggleModel}
          onDiscover={handleDiscover}
        />
      )}
    </div>
  )
}