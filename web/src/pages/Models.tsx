import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState, useMemo, useCallback } from 'react'
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

const CAP_META: { key: CapKey; label: string; style: string; muted: string }[] = [
  { key: 'vision', label: 'Vision', style: 'bg-purple-900/40 text-purple-300 border-purple-700/50', muted: 'bg-purple-900/15 text-purple-500/60 border-purple-700/25 hover:bg-purple-900/25 hover:text-purple-400' },
  { key: 'reasoning', label: 'Reasoning', style: 'bg-amber-900/40 text-amber-300 border-amber-700/50', muted: 'bg-amber-900/15 text-amber-500/60 border-amber-700/25 hover:bg-amber-900/25 hover:text-amber-400' },
  { key: 'tool_calling', label: 'Tools', style: 'bg-cyan-900/40 text-cyan-300 border-cyan-700/50', muted: 'bg-cyan-900/15 text-cyan-500/60 border-cyan-700/25 hover:bg-cyan-900/25 hover:text-cyan-400' },
  { key: 'structured_output', label: 'Structured', style: 'bg-emerald-900/40 text-emerald-300 border-emerald-700/50', muted: 'bg-emerald-900/15 text-emerald-500/60 border-emerald-700/25 hover:bg-emerald-900/25 hover:text-emerald-400' },
  { key: 'pdf_upload', label: 'PDF', style: 'bg-red-900/40 text-red-300 border-red-700/50', muted: 'bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400' },
  { key: 'video_input', label: 'Video', style: 'bg-pink-900/40 text-pink-300 border-pink-700/50', muted: 'bg-pink-900/15 text-pink-500/60 border-pink-700/25 hover:bg-pink-900/25 hover:text-pink-400' },
  { key: 'audio_input', label: 'Audio', style: 'bg-orange-900/40 text-orange-300 border-orange-700/50', muted: 'bg-orange-900/15 text-orange-500/60 border-orange-700/25 hover:bg-orange-900/25 hover:text-orange-400' },
  { key: 'parallel_tool_calls', label: 'Parallel', style: 'bg-teal-900/40 text-teal-300 border-teal-700/50', muted: 'bg-teal-900/15 text-teal-500/60 border-teal-700/25 hover:bg-teal-900/25 hover:text-teal-400' },
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

function SortHeader({ label, field, sort, onSort }: { label: string; field: SortField; sort: { field: SortField; dir: SortDir }; onSort: (f: SortField) => void }) {
  const active = sort.field === field
  return (
    <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider cursor-pointer select-none hover:text-gray-200 text-gray-400 whitespace-nowrap" onClick={() => onSort(field)}>
      {label} <span className="inline-block w-3 text-center">{active ? (sort.dir === 'asc' ? '↑' : '↓') : ' '}</span>
    </th>
  )
}

function ModelDetailModal({ model, onClose }: { model: Model; onClose: () => void }) {
  const caps = parseCapabilities(model.capabilities)
  const params = parseParams(model.params)
  const inputMods = (() => { try { return JSON.parse(model.input_modalities) as string[] } catch { return [] } })()
  const outputMods = (() => { try { return JSON.parse(model.output_modalities) as string[] } catch { return [] } })()

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
            <span className="text-gray-500">Owned By</span>
            <p className="text-gray-200">{model.owned_by || '-'}</p>
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
            <span className="text-gray-500">Modality</span>
            <p className="text-gray-200">{model.modality || '-'}</p>
          </div>
          <div>
            <span className="text-gray-500">Last Discovered</span>
            <p className="text-gray-200">{formatRelativeTime(model.last_seen_at)}</p>
          </div>
        </div>

        {(inputMods.length > 0 || outputMods.length > 0) && (
          <div className="mb-4">
            <h3 className="text-sm font-medium text-gray-400 mb-2">I/O</h3>
            <p className="text-sm text-gray-300">
              <span className="text-gray-500">In:</span> {inputMods.join(', ') || '-'}
              {outputMods.length > 0 && (
                <>
                  <span className="mx-2 text-gray-600">&rarr;</span>
                  <span className="text-gray-500">Out:</span> {outputMods.join(', ')}
                </>
              )}
            </p>
          </div>
        )}

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
          <span className={`px-2 py-1 text-xs rounded-full ${
            model.enabled ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'
          }`}>
            {model.enabled ? 'Enabled' : 'Disabled'}
          </span>
        </div>
      </div>
    </div>
  )
}

export function Models() {
  const { toast } = useToast()
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedProvider, setSelectedProvider] = useState<string>('')
  const [detailModel, setDetailModel] = useState<Model | null>(null)
  const [sort, setSort] = useState<{ field: SortField; dir: SortDir }>({ field: 'name', dir: 'asc' })
  const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set())

  const { data: models, isLoading } = useQuery({
    queryKey: ['models', selectedProvider],
    queryFn: () => api.models.list(selectedProvider || undefined),
  })

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const copyModelId = useCallback((modelId: string) => {
    navigator.clipboard.writeText(modelId).then(() => {
      toast(`Copied: ${modelId}`, 'info')
    }).catch(() => {
      toast('Failed to copy', 'error')
    })
  }, [toast])

  const toggleCapFilter = (key: CapKey) => {
    setCapFilter(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const handleSort = (field: SortField) => {
    setSort(prev => ({
      field,
      dir: prev.field === field && prev.dir === 'asc' ? 'desc' : 'asc',
    }))
  }

  const sortedAndFiltered = useMemo(() => {
    let result = models?.filter((model) =>
      model.model_id.toLowerCase().includes(searchQuery.toLowerCase()) ||
      model.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
      model.display_name?.toLowerCase().includes(searchQuery.toLowerCase())
    ) || []

    if (capFilter.size > 0) {
      result = result.filter(m => {
        const caps = parseCapabilities(m.capabilities)
        return Array.from(capFilter).some(k => hasCap(caps, k))
      })
    }

    const dir = sort.dir === 'asc' ? 1 : -1
    result.sort((a, b) => {
      switch (sort.field) {
        case 'name': return dir * (a.name || a.model_id).localeCompare(b.name || b.model_id)
        case 'provider': return dir * a.provider_name.localeCompare(b.provider_name)
        case 'discovered': return dir * (new Date(a.last_seen_at).getTime() - new Date(b.last_seen_at).getTime())
        case 'status': return dir * (a.enabled === b.enabled ? 0 : a.enabled ? -1 : 1)
        default: return 0
      }
    })

    return result
  }, [models, searchQuery, sort, capFilter])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500"></div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-3xl font-bold text-white">Models</h1>
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
                  {CAP_META.map(m => (
                    <button
                      key={m.key}
                      type="button"
                      onClick={() => toggleCapFilter(m.key)}
                      className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${capFilter.has(m.key) ? m.style : m.muted}`}
                    >
                      {m.label}
                    </button>
                  ))}
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
              <SortHeader label="Status" field="status" sort={sort} onSort={handleSort} />
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-700/50">
            {sortedAndFiltered.length > 0 ? (
              sortedAndFiltered.map((model) => {
                const caps = parseCapabilities(model.capabilities)
                return (
                  <tr key={model.id} className="hover:bg-gray-750">
                    <td className="px-4 py-1.5">
                      <button type="button" onClick={() => setDetailModel(model)} className="text-left hover:underline text-sm font-medium text-blue-400">
                        {model.name || model.model_id}
                      </button>
                      <button
                        type="button"
                        className="text-[11px] text-gray-500 font-mono leading-tight cursor-pointer hover:text-gray-300 transition-colors"
                        onClick={() => copyModelId(model.model_id)}
                        title="Click to copy model ID"
                      >
                        {model.model_id}
                      </button>
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
        <ModelDetailModal model={detailModel} onClose={() => setDetailModel(null)} />
      )}
    </div>
  )
}