import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'

export function Dashboard() {
  const { data: stats, isLoading: statsLoading, error: statsError } = useQuery({
    queryKey: ['stats'],
    queryFn: () => api.stats.get(),
    retry: 1,
  })

  const { data: models } = useQuery({
    queryKey: ['models'],
    queryFn: () => api.models.list(),
  })

  useQuery({
    queryKey: ['check-auth'],
    queryFn: async () => {
      const response = await fetch('/api/stats', { headers: { 'Authorization': `Bearer ${localStorage.getItem('adminToken')}` } })
      if (response.status === 401) {
        localStorage.removeItem('adminToken')
        window.location.reload()
      }
      return null
    },
    enabled: !!statsError,
  })

  if (statsError) {
    const errMsg = statsError.message || ''
    if (errMsg.includes('401') || errMsg.includes('Unauthorized') || errMsg.includes('Admin token')) {
      localStorage.removeItem('adminToken')
      window.location.reload()
    }
  }

  if (statsLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2" style={{ borderColor: 'var(--accent)' }}></div>
      </div>
    )
  }

  if (statsError) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold text-white">Dashboard</h1>
          <p className="text-gray-400 mt-1">Overview of your LLM proxy usage</p>
        </div>
        <div className="bg-red-900/50 border border-red-700 rounded-lg p-6 text-red-300">
          Failed to load stats: {statsError.message}
        </div>
      </div>
    )
  }

  const statCards = [
    { id: 'models', label: 'Total Models', value: models?.length || 0, icon: '🤖' },
    { id: 'req24h', label: 'Requests (24h)', value: stats?.total_requests_last_24h || 0, icon: '📊' },
    { id: 'req7d', label: 'Requests (7d)', value: stats?.total_requests_last_7d || 0, icon: '📈' },
    { id: 'latency', label: 'Avg Duration', value: `${(stats?.avg_latency_ms || 0).toFixed(1)}ms`, icon: '⚡' },
    { id: 'errors', label: 'Error Rate', value: `${((stats?.error_rate || 0) * 100).toFixed(1)}%`, icon: '❌' },
    { id: 'tokens', label: 'Total Tokens', value: (stats?.total_tokens_prompt || 0) + (stats?.total_tokens_completion || 0), icon: '🎯' },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-white">Dashboard</h1>
        <p className="text-gray-400 mt-1">Overview of your LLM proxy usage</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
        {statCards.map((card) => (
          <div key={card.id} className="ui-card p-5">
            <div className="flex items-center justify-between mb-3">
              <span className="text-2xl">{card.icon}</span>
              <span className="text-xs text-gray-500 uppercase tracking-wide">{card.label}</span>
            </div>
            <p className="text-2xl font-bold text-white">{card.value}</p>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="ui-card p-6">
          <h3 className="text-lg font-semibold text-white mb-4">Usage by Model</h3>
          {stats && Object.keys(stats.by_model).length > 0 ? (
            <div className="space-y-3">
              {Object.entries(stats.by_model)
                .sort(([, a], [, b]) => b - a)
                .slice(0, 5)
                .map(([model, count]) => (
                  <div key={model} className="flex justify-between items-center">
                    <span className="text-gray-300 truncate">{model}</span>
                    <span className="font-semibold text-white">{count}</span>
                  </div>
                ))}
            </div>
          ) : (
            <p className="text-gray-500 text-center py-8">No usage data available</p>
          )}
        </div>

        <div className="ui-card p-6">
          <h3 className="text-lg font-semibold text-white mb-4">Usage by Provider</h3>
          {stats && Object.keys(stats.by_provider).length > 0 ? (
            <div className="space-y-3">
              {Object.entries(stats.by_provider)
                .sort(([, a], [, b]) => b - a)
                .slice(0, 5)
                .map(([provider, count]) => (
                  <div key={provider} className="flex justify-between items-center">
                    <span className="text-gray-300 truncate">{provider}</span>
                    <span className="font-semibold text-white">{count}</span>
                  </div>
                ))}
            </div>
          ) : (
            <p className="text-gray-500 text-center py-8">No usage data available</p>
          )}
        </div>

        <div className="ui-card p-6">
          <h3 className="text-lg font-semibold text-white mb-4">Usage by Virtual Key</h3>
          {stats && Object.keys(stats.by_virtual_key).length > 0 ? (
            <div className="space-y-3">
              {Object.entries(stats.by_virtual_key)
                .sort(([, a], [, b]) => b - a)
                .slice(0, 5)
                .map(([name, tokens]) => (
                  <div key={name} className="flex justify-between items-center">
                    <span className="text-gray-300 truncate">{name}</span>
                    <span className="font-semibold text-white">{tokens.toLocaleString()} tokens</span>
                  </div>
                ))}
            </div>
          ) : (
            <p className="text-gray-500 text-center py-8">No usage data available</p>
          )}
        </div>
      </div>
    </div>
  )
}