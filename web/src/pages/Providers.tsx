import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState, useMemo } from 'react'
import type { NanoGPTUsage } from '../api/types'
import { useToast } from '../context/ToastContext'

function formatTokens(n: number | null | undefined): string {
  if (n == null) return '-'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toString()
}

function formatTimestamp(ts: number): string {
  return new Date(ts).toLocaleString()
}

function formatTimeUntil(ts: number): string {
  const now = Date.now()
  const diff = ts - now
  if (diff <= 0) return 'now'

  const hours = Math.floor(diff / (1000 * 60 * 60))
  const days = Math.floor(hours / 24)
  const remainingHours = hours % 24

  if (days > 0) {
    const dayLabel = days === 1 ? 'day' : 'days'
    const hourLabel = remainingHours === 1 ? 'hour' : 'hours'
    return `in ${days} ${dayLabel}, ${remainingHours} ${hourLabel}`
  }
  const hourLabel = hours === 1 ? 'hour' : 'hours'
  return `in ${hours} ${hourLabel}`
}

function NanoGPTQuotaModal({ usage, onClose, onRefresh, isRefreshing, onToast }: { usage: NanoGPTUsage; onClose: () => void; onRefresh: () => Promise<unknown>; isRefreshing: boolean; onToast: (msg: string, type: 'success' | 'error' | 'info') => void }) {
  const weeklyLimit = usage.limits.weeklyInputTokens ?? 0
  const weeklyUsed = usage.weeklyInputTokens?.used ?? 0
  const weeklyPercent = weeklyLimit > 0 ? (weeklyUsed / weeklyLimit) * 100 : 0

  const handleRefresh = async () => {
    try {
      await onRefresh()
      onToast('Quota refreshed', 'success')
    } catch {
      onToast('Failed to refresh quota', 'error')
    }
  }

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
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleRefresh}
              disabled={isRefreshing}
              className="text-gray-400 hover:text-white text-lg leading-none p-1 rounded hover:bg-gray-700 transition-colors disabled:opacity-50"
              aria-label="Refresh"
              title="Refresh quota info"
            >
              <svg className={`w-5 h-5 ${isRefreshing ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
            </button>
            <button type="button" onClick={onClose} className="text-gray-400 hover:text-white text-xl leading-none" aria-label="Close">&times;</button>
          </div>
        </div>

        <div className="space-y-6">
          <div>
            <div className="flex justify-between items-center mb-2">
              <span className="text-sm font-medium text-gray-300">Weekly Token Quota</span>
              <span className="text-sm text-gray-400">{formatTokens(weeklyUsed)} / {formatTokens(weeklyLimit)}</span>
            </div>
            <div className="w-full bg-gray-700 rounded-full h-3">
              <div
                className="bg-[#0690a8] h-3 rounded-full transition-all"
                style={{ width: `${Math.min(weeklyPercent, 100)}%` }}
              />
            </div>
            <p className="text-xs text-gray-500 mt-1">{weeklyPercent.toFixed(1)}% used. Resets {usage.weeklyInputTokens?.resetAt ? `${formatTimestamp(usage.weeklyInputTokens.resetAt)} - ${formatTimeUntil(usage.weeklyInputTokens.resetAt)}` : 'N/A'}</p>
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
              <p className="text-xs text-gray-500 mt-1">{usage.dailyImages.percentUsed.toFixed(1)}% used. Resets {usage.dailyImages.resetAt ? `${formatTimestamp(usage.dailyImages.resetAt)} - ${formatTimeUntil(usage.dailyImages.resetAt)}` : 'N/A'}</p>
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
              <p className="text-xs text-gray-500 mt-1">{usage.dailyInputTokens.percentUsed.toFixed(1)}% used. Resets {usage.dailyInputTokens.resetAt ? `${formatTimestamp(usage.dailyInputTokens.resetAt)} - ${formatTimeUntil(usage.dailyInputTokens.resetAt)}` : 'N/A'}</p>
            </div>
          )}

          <div>
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
      </div>
    </div>
  )
}

export function Providers() {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [showModal, setShowModal] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [discoveringId, setDiscoveringId] = useState<string | null>(null)
  const [quotaUsage, setQuotaUsage] = useState<NanoGPTUsage | null>(null)
  const [formData, setFormData] = useState<{
    name: string;
    base_url: string;
    api_key: string;
    provider_type: string;
  }>({
    name: '',
    base_url: '',
    api_key: '',
    provider_type: 'openai',
  })

  const { data: providers, isLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.settings.get(),
  })

  const nanogptProviderId = useMemo(() => {
    return providers?.find(p => p.base_url.includes('nano-gpt.com'))?.id
  }, [providers])

  const deepseekProviderId = useMemo(() => {
    return providers?.find(p => p.base_url.includes('deepseek.com'))?.id
  }, [providers])

  const { data: nanogptUsage, refetch, isRefetching } = useQuery({
    queryKey: ['nanogpt-usage', nanogptProviderId],
    queryFn: () => api.providers.getUsage(nanogptProviderId!),
    enabled: Boolean(nanogptProviderId),
    refetchInterval: 60 * 60 * 1000,
  })

  const { data: deepseekBalanceData, refetch: refetchDeepseekBalance } = useQuery({
    queryKey: ['deepseek-balance', deepseekProviderId],
    queryFn: () => api.providers.getBalance(deepseekProviderId!),
    enabled: Boolean(deepseekProviderId),
    refetchInterval: 60 * 60 * 1000,
  })

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

  const createMutation = useMutation({
    mutationFn: (data: { name: string; base_url: string; api_key: string }) => api.providers.create(data),
    onSuccess: async (newProvider) => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      setShowModal(false)
      setFormData({ name: '', base_url: '', api_key: '', provider_type: 'custom' })
      setError(null)
      toast(`Provider "${newProvider.name}" added`, 'success')
      const shouldDiscover = settings?.discovery_on_provider_create !== 'false'
      if (shouldDiscover) {
        try {
          toast('Discovering models...', 'info')
          const result = await api.providers.discover(newProvider.id)
          queryClient.invalidateQueries({ queryKey: ['models'] })
          queryClient.invalidateQueries({ queryKey: ['providers'] })
          toast(`Discovered ${result?.discovered ?? 'new'} models`, 'success')
        } catch {
          toast('Auto-discovery failed', 'error')
        }
      }
    },
    onError: (err: Error) => {
      setError(err.message)
      toast(`Failed to add provider: ${err.message}`, 'error')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.providers.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      queryClient.invalidateQueries({ queryKey: ['models'] })
      toast('Provider deleted', 'success')
    },
    onError: (err: Error) => {
      toast(`Failed to delete: ${err.message}`, 'error')
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    createMutation.mutate({ name: formData.name, base_url: formData.base_url, api_key: formData.api_key })
  }

  const handleProviderTypeChange = (type: string) => {
    const baseUrls: Record<string, string> = {
      nanogpt: 'https://api.nano-gpt.com/v1',
      'z-ai': 'https://api.z.ai/api/paas/v4',
      openai: 'https://api.openai.com/v1',
      deepseek: 'https://api.deepseek.com/v1',
    }
    setFormData(prev => ({
      ...prev,
      provider_type: type,
      base_url: baseUrls[type] || prev.base_url,
    }))
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-indigo-400"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold text-white">Providers</h1>
          <p className="text-gray-400 mt-1">Manage your LLM provider configurations</p>
        </div>
        <button
          type="button"
          onClick={() => setShowModal(true)}
          className="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors font-medium"
        >
          + Add Provider
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {providers?.map((provider) => {
          const isNanoGPT = provider.base_url.includes('nano-gpt.com')
          const weeklyUsed = isNanoGPT ? nanogptUsage?.weeklyInputTokens?.used : null
          const weeklyLimit = isNanoGPT ? nanogptUsage?.limits?.weeklyInputTokens : null
          const showQuotaBadge = isNanoGPT && weeklyUsed != null && weeklyLimit && nanogptUsage

          return (
          <div key={provider.id} className="bg-gray-800 border border-gray-700 rounded-xl p-6">
            <div className="mb-4">
              <div className="flex items-center justify-between">
                <h3 className="text-lg font-semibold text-white">{provider.name}</h3>
              </div>
              <p className="text-sm text-gray-400 mt-1 truncate">{provider.base_url}</p>
            </div>

            <div className="space-y-2 text-sm">
              <div className="flex justify-between">
                <span className="text-gray-500">API Key</span>
                <span className="font-mono text-gray-300">{provider.masked_key}</span>
              </div>
              {provider.last_discovered_at && (
                <div className="flex justify-between">
                  <span className="text-gray-500">Last Discovery</span>
                  <span className="text-gray-300">{new Date(provider.last_discovered_at).toLocaleString()}</span>
                </div>
              )}
            </div>

            <div className="mt-4 flex items-center justify-between gap-2">
              {showQuotaBadge && (
                <button
                  type="button"
                  onClick={() => nanogptUsage && setQuotaUsage(nanogptUsage)}
                  className="px-2 py-1.5 rounded-full bg-[#0690a8]/20 text-[#0690a8] border border-[#0690a8]/50 text-xs font-medium cursor-pointer hover:bg-[#0690a8]/30 transition-colors"
                  title="View quota details"
                >
                  {formatTokens(weeklyUsed)}/{formatTokens(weeklyLimit)}
                </button>
              )}
              {provider.base_url.includes('deepseek.com') && deepseekBalanceData && (
                <button
                  type="button"
                  onClick={async () => {
                    try {
                      await refetchDeepseekBalance()
                      toast('Balance refreshed', 'success')
                    } catch {
                      toast('Failed to refresh balance', 'error')
                    }
                  }}
                  className="px-2 py-1.5 rounded-full bg-[#36aaff]/20 text-[#36aaff] border border-[#36aaff]/50 text-xs font-medium cursor-pointer hover:bg-[#36aaff]/30 transition-colors"
                  title="Refresh balance"
                >
                  {deepseekBalanceData.balance_infos.find(b => b.currency === 'USD')?.total_balance ?? '-'} USD
                </button>
              )}
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => discoverMutation.mutate(provider.id)}
                  disabled={discoveringId !== null}
                  className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
                    discoveringId === provider.id
                      ? 'bg-indigo-900/20 text-indigo-500/50 border-indigo-700/20 cursor-not-allowed'
                      : discoveringId !== null
                      ? 'bg-gray-800/50 text-gray-600 border-gray-700/30 cursor-not-allowed'
                      : 'bg-indigo-900/40 text-indigo-300 border-indigo-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(129,140,248,0.2)]'
                  }`}
                >
                  {discoveringId === provider.id ? 'Discovering...' : 'Discover Models'}
                </button>
                <button
                  type="button"
                  onClick={() => deleteMutation.mutate(provider.id)}
                  className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] cursor-pointer transition-all"
                >
                  Delete
                </button>
              </div>
            </div>
          </div>
          )
        })}

        {providers?.length === 0 && (
          <div className="col-span-full text-center py-12 bg-gray-800 border border-gray-700 rounded-xl">
            <p className="text-gray-500">No providers configured. Add your first provider to get started.</p>
          </div>
        )}
      </div>

      {showModal && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
          <div className="bg-gray-800 border border-gray-700 rounded-2xl p-6 w-full max-w-md">
            <h2 className="text-xl font-bold text-white mb-4">Add Provider</h2>

            {error && (
              <div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
                {error}
              </div>
            )}

            <form onSubmit={handleSubmit} className="space-y-4">
              <div>
                <label htmlFor="provider-type" className="block text-sm font-medium text-gray-300 mb-1">
                  Type
                </label>
                <select
                  id="provider-type"
                  value={formData.provider_type}
                  onChange={(e) => handleProviderTypeChange(e.target.value)}
                  className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
                >
                  <option value="openai">OpenAI Compatible</option>
                  <option value="nanogpt">NanoGPT</option>
                  <option value="z-ai">Z.ai</option>
                  <option value="deepseek">DeepSeek</option>
                </select>
              </div>

              <div>
                <label htmlFor="provider-name" className="block text-sm font-medium text-gray-300 mb-1">
                  Name
                </label>
                <input
                  id="provider-name"
                  type="text"
                  required
                  autoFocus={true}
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
                  placeholder="e.g., OpenAI"
                />
              </div>

              <div>
                <label htmlFor="provider-base-url" className="block text-sm font-medium text-gray-300 mb-1">
                  Base URL
                </label>
                <input
                  id="provider-base-url"
                  type="url"
                  required
                  value={formData.base_url}
                  onChange={(e) => setFormData({ ...formData, base_url: e.target.value })}
                  className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
                  placeholder="https://api.openai.com/v1"
                />
                <p className="text-gray-500 text-xs mt-1">Full API base URL including any path prefix. Models will be discovered from {'<base_url>'}/models</p>
              </div>

              <div>
                <label htmlFor="provider-api-key" className="block text-sm font-medium text-gray-300 mb-1">
                  API Key
                </label>
                <input
                  id="provider-api-key"
                  type="password"
                  required
                  value={formData.api_key}
                  onChange={(e) => setFormData({ ...formData, api_key: e.target.value })}
                  className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
                  placeholder="API key"
                />
              </div>

              <div className="flex space-x-3 justify-end pt-4">
                <button
                  type="button"
                  onClick={() => {
                    setShowModal(false)
                    setFormData({ name: '', base_url: '', api_key: '', provider_type: 'custom' })
                    setError(null)
                  }}
                  className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={createMutation.isPending}
                  className="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors disabled:opacity-50"
                >
                  {createMutation.isPending ? 'Adding...' : 'Add Provider'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {quotaUsage && (
        <NanoGPTQuotaModal
          usage={quotaUsage}
          onClose={() => setQuotaUsage(null)}
          onRefresh={refetch}
          isRefreshing={isRefetching}
          onToast={toast}
        />
      )}
    </div>
  )
}