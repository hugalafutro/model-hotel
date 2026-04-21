import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState, useMemo, useCallback, useEffect, useRef } from 'react'
import type { Model, ModelCapabilities, NanoGPTUsage } from '../api/types'
import { useToast } from '../context/ToastContext'
import { SortableHeader, Row, EmptyRow } from '../components/DataTable'
import type { SortState } from '../components/DataTable'

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

function formatTokens(n: number | null | undefined): string {
  if (n == null) return '-'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toString()
}

function formatTimestamp(ts: number): string {
  return new Date(ts).toLocaleString()
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

type SortField = 'name' | 'capabilities' | 'provider' | 'discovered' | 'context' | 'output' | 'status'
type StatusFilter = 'enabled' | 'disabled'

function ModelDetailModal({ model, onClose, onToggle, onDiscover, onTest, onToast }: {
  model: Model
  onClose: () => void
  onToggle: (id: string, enabled: boolean) => void
  onDiscover: (providerId: string) => Promise<unknown>
  onTest: (id: string) => Promise<{success: boolean; ttft_ms: number; duration_ms: number; response: string; error?: string}>
  onToast: (msg: string, type?: 'success' | 'error' | 'info') => void
}) {
  const caps = parseCapabilities(model.capabilities)
  const params = parseParams(model.params)
  const inputMods = (() => { try { const v = JSON.parse(model.input_modalities); return Array.isArray(v) ? v : [v] } catch { return [] } })()
  const outputMods = (() => { try { const v = JSON.parse(model.output_modalities); return Array.isArray(v) ? v : [v] } catch { return [] } })()
  const [cooldown, setCooldown] = useState(0)
  const [discovering, setDiscovering] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testError, setTestError] = useState(false)
  const [snippetTab, setSnippetTab] = useState<'curl' | 'zed'>('curl')
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

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

  const handleTest = async () => {
    if (testing) return
    setTesting(true)
    setTestError(false)
    try {
      const result = await onTest(model.id)
      if (result.success) {
        const content = result.response.replace(/\n/g, ' ').slice(0, 80)
        onToast(`Success | Response: ${content} | TTFT: ${(result.ttft_ms / 1000).toFixed(1)}s | Duration: ${(result.duration_ms / 1000).toFixed(1)}s`, 'success')
      } else {
        setTestError(true)
        onToast(`Test failed: ${result.error || 'Unknown error'}`, 'error')
        setTimeout(() => setTestError(false), 3000)
      }
    } catch (err) {
      setTestError(true)
      onToast(`Test failed: ${err instanceof Error ? err.message : 'Unknown error'}`, 'error')
      setTimeout(() => setTestError(false), 3000)
    } finally {
      setTesting(false)
    }
  }

  const curlCmd = `curl -X POST ${window.location.origin}/v1/chat/completions \\\n  -H "Authorization: Bearer API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${model.model_id}","messages":[{"role":"user","content":"Hello"}]}'`

  const zedJson = JSON.stringify({
    name: model.model_id,
    display_name: model.name,
    max_tokens: model.context_length,
    max_output_tokens: model.max_output_tokens,
    capabilities: {
      tools: hasCap(caps, 'tool_calling'),
      images: hasCap(caps, 'vision'),
      parallel_tool_calls: hasCap(caps, 'parallel_tool_calls'),
      prompt_cache_key: false,
    },
  }, null, 2)

  const snippetContent = snippetTab === 'curl' ? curlCmd : zedJson

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

        <div className="mt-4 pt-4 border-t border-gray-700">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-1">
              {(['curl', 'zed'] as const).map(tab => (
                <button
                  key={tab}
                  type="button"
                  onClick={() => setSnippetTab(tab)}
                  className={`px-2.5 py-1 rounded text-[11px] font-medium uppercase tracking-wider cursor-pointer transition-all ${
                    snippetTab === tab
                      ? 'bg-slate-700/60 text-slate-200 border border-slate-600/50'
                      : 'text-slate-500 hover:text-slate-400 border border-transparent'
                  }`}
                >
                  {tab === 'curl' ? 'cURL' : 'ZED'}
                </button>
              ))}
            </div>
            <button
              type="button"
              onClick={() => { navigator.clipboard.writeText(snippetContent); onToast('Copied to clipboard', 'info') }}
              className="px-1.5 py-0.5 rounded text-[10px] font-medium border bg-slate-700/40 text-slate-300 border-slate-600/40 hover:brightness-125 transition-all cursor-pointer"
            >
              Copy
            </button>
          </div>
          <pre className="bg-gray-950 rounded-lg p-3 text-[11px] text-gray-300 font-mono overflow-x-auto leading-relaxed whitespace-pre-wrap break-all">{snippetContent}</pre>
        </div>

        <div className="flex items-center justify-between mt-4 pt-4">
          <button
            type="button"
            onClick={() => onToggle(model.id, !model.enabled)}
            className={`px-3 py-1.5 text-xs rounded-full border cursor-pointer transition-all ${
              model.enabled
                ? 'bg-green-900/50 text-green-400 border-green-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(34,197,94,0.2)]'
                : 'bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)]'
            }`}
          >
            {model.enabled ? 'Enabled' : 'Disabled'}
          </button>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={testing}
              onClick={handleTest}
              className={`px-3 py-1.5 text-xs rounded-full border transition-all flex items-center gap-1.5 ${
                testError
                  ? 'bg-red-900/50 text-red-300 border-red-700/50'
                  : testing
                    ? 'bg-amber-900/30 text-amber-300/70 border-amber-700/30 cursor-wait'
                    : 'bg-amber-900/40 text-amber-300 border-amber-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(245,158,11,0.2)]'
              }`}
            >
              {testing && (
                <span className="inline-block w-3 h-3 border-2 border-amber-400/50 border-t-amber-300 rounded-full animate-spin" />
              )}
              {testing ? 'Testing...' : 'Test'}
            </button>
            <button
              type="button"
              disabled={cooldown > 0 || discovering}
              onClick={handleDiscover}
              className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
                cooldown > 0 || discovering
                  ? 'bg-indigo-900/20 text-indigo-500/50 border-indigo-700/20 cursor-not-allowed'
                  : 'bg-indigo-900/40 text-indigo-300 border-indigo-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(129,140,248,0.2)]'
              }`}
            >
              {discovering ? 'Updating...' : cooldown > 0 ? `Update (${cooldown}s)` : 'Update info'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function NanoGPTQuotaModal({ usage, onClose }: { usage: NanoGPTUsage; onClose: () => void }) {
  const weeklyLimit = usage.limits.weeklyInputTokens ?? 0
  const weeklyUsed = usage.weeklyInputTokens?.used ?? 0
  const weeklyPercent = weeklyLimit > 0 ? (weeklyUsed / weeklyLimit) * 100 : 0

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 flex items-center justify-center z-50" onKeyDown={(e) => { if (e.key === 'Escape') onClose() }}>
      <button type="button" className="absolute inset-0 bg-black/60 cursor-default" onClick={onClose} aria-label="Close dialog" />
      <div className="relative bg-gray-800 border border-gray-700 rounded-2xl p-6 w-full max-w-md max-h-[85vh] overflow-y-auto">
        <div className="flex justify-between items-start mb-6">
          <div>
            <h2 className="text-xl font-bold text-white">NanoGPT Subscription</h2>
            <p className="text-sm text-gray-400 mt-1">
              {usage.active ? (
                <span className="inline-flex items-center gap-1.5">
                  <span className="w-2 h-2 rounded-full bg-green-400"></span>
                  Active
                </span>
              ) : (
                <span className="inline-flex items-center gap-1.5">
                  <span className="w-2 h-2 rounded-full bg-red-400"></span>
                  Inactive
                </span>
              )}
            </p>
          </div>
          <button type="button" onClick={onClose} className="text-gray-400 hover:text-white text-xl leading-none" aria-label="Close">&times;</button>
        </div>

        <div className="space-y-6">
          <div>
            <div className="flex justify-between items-center mb-2">
              <span className="text-sm font-medium text-gray-300">Weekly Token Quota</span>
              <span className="text-sm text-gray-400">{formatTokens(weeklyUsed)} / {formatTokens(weeklyLimit)}</span>
            </div>
            <div className="w-full bg-gray-700 rounded-full h-3">
              <div
                className="bg-indigo-500 h-3 rounded-full transition-all"
                style={{ width: `${Math.min(weeklyPercent, 100)}%` }}
              />
            </div>
            <p className="text-xs text-gray-500 mt-1">{weeklyPercent.toFixed(1)}% used. Resets {usage.weeklyInputTokens?.resetAt ? formatTimestamp(usage.weeklyInputTokens.resetAt) : 'N/A'}</p>
          </div>

          {usage.dailyImages && (
            <div>
              <div className="flex justify-between items-center mb-2">
                <span className="text-sm font-medium text-gray-300">Daily Images</span>
                <span className="text-sm text-gray-400">{usage.dailyImages.used} / {usage.limits.dailyImages ?? '∞'}</span>
              </div>
              <div className="w-full bg-gray-700 rounded-full h-3">
                <div
                  className="bg-purple-500 h-3 rounded-full transition-all"
                  style={{ width: `${Math.min(usage.dailyImages.percentUsed * 100, 100)}%` }}
                />
              </div>
              <p className="text-xs text-gray-500 mt-1">{usage.dailyImages.percentUsed.toFixed(1)}% used. Resets {usage.dailyImages.resetAt ? formatTimestamp(usage.dailyImages.resetAt) : 'N/A'}</p>
            </div>
          )}

          {usage.dailyInputTokens && (
            <div>
              <div className="flex justify-between items-center mb-2">
                <span className="text-sm font-medium text-gray-300">Daily Input Tokens</span>
                <span className="text-sm text-gray-400">{formatTokens(usage.dailyInputTokens.used)} / {usage.limits.dailyInputTokens ? formatTokens(usage.limits.dailyInputTokens) : '∞'}</span>
              </div>
              <div className="w-full bg-gray-700 rounded-full h-3">
                <div
                  className="bg-amber-500 h-3 rounded-full transition-all"
                  style={{ width: `${Math.min(usage.dailyInputTokens.percentUsed * 100, 100)}%` }}
                />
              </div>
              <p className="text-xs text-gray-500 mt-1">{usage.dailyInputTokens.percentUsed.toFixed(1)}% used. Resets {usage.dailyInputTokens.resetAt ? formatTimestamp(usage.dailyInputTokens.resetAt) : 'N/A'}</p>
            </div>
          )}

          <div className="border-t border-gray-700 pt-4">
            <h3 className="text-sm font-medium text-gray-300 mb-3">Subscription Details</h3>
            <div className="grid grid-cols-2 gap-3 text-sm">
              <div>
                <span className="text-gray-500">Provider</span>
                <p className="text-gray-200 capitalize">{usage.provider}</p>
              </div>
              <div>
                <span className="text-gray-500">Status</span>
                <p className="text-gray-200 capitalize">{usage.providerStatus}</p>
              </div>
              <div>
                <span className="text-gray-500">Period End</span>
                <p className="text-gray-200">{new Date(usage.period.currentPeriodEnd).toLocaleDateString()}</p>
              </div>
              <div>
                <span className="text-gray-500">Allow Overage</span>
                <p className="text-gray-200">{usage.allowOverage ? 'Yes' : 'No'}</p>
              </div>
            </div>
          </div>

          {usage.cancelAtPeriodEnd && (
            <div className="p-3 bg-yellow-900/30 border border-yellow-700/50 rounded-lg">
              <p className="text-sm text-yellow-300">
                Subscription will cancel at period end ({new Date(usage.period.currentPeriodEnd).toLocaleDateString()})
              </p>
            </div>
          )}
        </div>

        <div className="mt-6 pt-4 border-t border-gray-700 flex justify-end">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors"
          >
            Close
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
  const [sort, setSort] = useState<SortState<SortField>>({ field: 'name', dir: 'asc' })
  const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set())
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('enabled')
  const [quotaProviderId, setQuotaProviderId] = useState<string | null>(null)

  const { data: models, isLoading } = useQuery({
    queryKey: ['models', selectedProvider],
    queryFn: () => api.models.list(selectedProvider || undefined),
  })

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const nanogptProviderId = useMemo(() => {
    return providers?.find(p => p.base_url.includes('nano-gpt.com'))?.id
  }, [providers])

  const { data: nanogptUsage } = useQuery({
    queryKey: ['nanogpt-usage', nanogptProviderId],
    queryFn: () => api.providers.getUsage(nanogptProviderId!),
    enabled: !!nanogptProviderId,
  })

  const { data: quotaUsage } = useQuery({
    queryKey: ['nanogpt-usage-modal', quotaProviderId],
    queryFn: () => api.providers.getUsage(quotaProviderId!),
    enabled: !!quotaProviderId,
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

  const handleTest = useCallback(async (id: string) => {
    return api.models.test(id)
  }, [])

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
        case 'context': return dir * ((a.context_length ?? 0) - (b.context_length ?? 0))
        case 'output': return dir * ((a.max_output_tokens ?? 0) - (b.max_output_tokens ?? 0))
        case 'status': return dir * (a.enabled === b.enabled ? 0 : a.enabled ? -1 : 1)
        default: return 0
      }
    })

    return { sortedAndFiltered: filtered, pillAvailability: availability, existingCaps: capsInData }
  }, [models, searchQuery, sort, capFilter, statusFilter])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-indigo-400"></div>
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
            autoFocus={true}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
          />
        </div>
        <div className="md:w-64">
          <select
            value={selectedProvider}
            onChange={(e) => setSelectedProvider(e.target.value)}
            className="hidden w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
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

      {providers && providers.length > 0 && (
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={() => setSelectedProvider('')}
            className={`inline-flex items-center px-3 py-1.5 rounded-full text-xs font-medium border transition-colors ${
              selectedProvider === ''
                ? 'bg-indigo-500/20 text-indigo-300 border-indigo-700/50'
                : 'bg-gray-800/60 text-gray-400 border-gray-700/40 hover:bg-gray-700/60 hover:text-gray-300'
            }`}
          >
            All
            <span className="ml-1.5 text-[10px] opacity-70">{models?.length ?? 0}</span>
          </button>
          {providers.map(provider => {
            const count = models?.filter(m => m.provider_id === provider.id).length ?? 0
            const isNanoGPT = provider.base_url.includes('nano-gpt.com')
            const isSelected = selectedProvider === provider.id
            const weeklyUsed = isNanoGPT ? nanogptUsage?.weeklyInputTokens?.used : null
            const weeklyLimit = isNanoGPT ? nanogptUsage?.limits?.weeklyInputTokens : null
            const showQuotaBadge = isNanoGPT && weeklyUsed != null && weeklyLimit

            return (
              <button
                key={provider.id}
                type="button"
                onClick={() => {
                  if (isNanoGPT) {
                    setQuotaProviderId(provider.id)
                  } else {
                    setSelectedProvider(provider.id)
                  }
                }}
                className={`inline-flex items-center px-3 py-1.5 rounded-full text-xs font-medium border transition-colors ${
                  isSelected
                    ? 'bg-indigo-500/20 text-indigo-300 border-indigo-700/50'
                    : 'bg-gray-800/60 text-gray-400 border-gray-700/40 hover:bg-gray-700/60 hover:text-gray-300'
                }`}
              >
                {provider.name}
                <span className="ml-1.5 text-[10px] opacity-70">{count}</span>
                {showQuotaBadge && (
                  <span
                    onClick={(e) => { e.stopPropagation(); setQuotaProviderId(provider.id) }}
                    className="ml-1.5 px-1.5 py-0.5 rounded-full bg-indigo-900/40 text-indigo-300 border border-indigo-700/50 text-[10px] font-medium cursor-pointer hover:bg-indigo-900/60 transition-colors"
                    title="View quota details"
                  >
                    {formatTokens(weeklyUsed)}/{formatTokens(weeklyLimit)}
                  </span>
                )}
              </button>
            )
          })}
        </div>
      )}

      <div className="border border-gray-700 rounded-xl overflow-hidden">
        <table className="min-w-full table-fixed">
          <colgroup>
            <col className="w-[22%]" />
            <col className="w-[26%]" />
            <col className="w-[12%]" />
            <col className="w-[11%]" />
            <col className="w-[9%]" />
            <col className="w-[9%]" />
            <col className="w-[11%]" />
          </colgroup>
          <thead>
            <tr className="bg-gray-800/80">
              <SortableHeader label="Model" field="name" sort={sort} onSort={handleSort} />
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
              <SortableHeader label="Provider" field="provider" sort={sort} onSort={handleSort} />
              <SortableHeader label="Discovered" field="discovered" sort={sort} onSort={handleSort} />
              <SortableHeader label="Ctx" field="context" sort={sort} onSort={handleSort} />
              <SortableHeader label="Max Out" field="output" sort={sort} onSort={handleSort} />
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
          <tbody>
            {sortedAndFiltered.length > 0 ? (
              sortedAndFiltered.map((model, idx) => {
                const caps = parseCapabilities(model.capabilities)
                return (
                  <Row key={model.id} index={idx}>
                    <td className="px-4 py-1.5">
                      <div className="flex flex-col">
                        <button type="button" onClick={() => setDetailModel(model)} className="text-left text-sm font-medium text-indigo-400 hover:text-indigo-300 cursor-pointer transition-colors">
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
                    <td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">{model.context_length ? model.context_length.toLocaleString() : '-'}</td>
                    <td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">{model.max_output_tokens ? model.max_output_tokens.toLocaleString() : '-'}</td>
                    <td className="px-4 py-1.5 whitespace-nowrap">
                      <span className={`px-2 py-0.5 text-xs rounded-full ${model.enabled ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}`}>
                        {model.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </td>
                  </Row>
                )
              })
            ) : (
              <EmptyRow colSpan={7} message={searchQuery || selectedProvider || capFilter.size > 0
                ? 'No models match your filters'
                : 'No models discovered yet. Add a provider and discover models.'} />
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
          onTest={handleTest}
          onToast={toast}
        />
      )}

      {quotaProviderId && quotaUsage && (
        <NanoGPTQuotaModal
          usage={quotaUsage}
          onClose={() => setQuotaProviderId(null)}
        />
      )}
    </div>
  )
}