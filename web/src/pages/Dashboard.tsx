import { useQuery } from '@tanstack/react-query'
import { api, setAdminToken } from '../api/client'
import { useEffect, useState } from 'react'

export function Dashboard() {
  const [adminToken, setAdminTokenInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [isLoggedIn, setIsLoggedIn] = useState(false)

  useEffect(() => {
    const token = localStorage.getItem('adminToken')
    if (token) {
      setAdminToken(token)
      setIsLoggedIn(true)
    }
  }, [])

  const { data: stats, isLoading: statsLoading, error: statsError } = useQuery({
    queryKey: ['stats'],
    queryFn: () => api.stats.get(),
    enabled: isLoggedIn,
    retry: 1,
  })

  // If stats query fails with auth error, log out
  useEffect(() => {
    if (statsError && isLoggedIn) {
      const errMsg = statsError.message || ''
      if (errMsg.includes('401') || errMsg.includes('Unauthorized') || errMsg.includes('Admin token')) {
        localStorage.removeItem('adminToken')
        setIsLoggedIn(false)
        setError('Session expired. Please log in again.')
      }
    }
  }, [statsError, isLoggedIn])

  const handleLogin = () => {
    if (!adminToken.trim()) {
      setError('Please enter an admin token')
      return
    }
    setError(null)
    localStorage.setItem('adminToken', adminToken.trim())
    setAdminToken(adminToken.trim())
    setIsLoggedIn(true)
  }

  const handleLogout = () => {
    localStorage.removeItem('adminToken')
    setIsLoggedIn(false)
    setError(null)
  }

  if (!isLoggedIn) {
    return (
      <div className="min-h-screen bg-gray-900 flex items-center justify-center">
        <div className="bg-gray-800 shadow-2xl rounded-2xl p-8 w-full max-w-md border border-gray-700">
          <div className="text-center mb-8">
            <h1 className="text-3xl font-bold text-white mb-2">LLM-Proxy</h1>
            <p className="text-gray-400">Multi-Provider LLM Proxy Dashboard</p>
          </div>

          {error && (
            <div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
              {error}
            </div>
          )}

          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-300 mb-2">
                Admin Token
              </label>
              <input
                type="password"
                value={adminToken}
                onChange={(e) => setAdminTokenInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleLogin()}
                className="w-full px-4 py-3 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
                placeholder="Enter your admin token"
              />
            </div>
            <button
              onClick={handleLogin}
              className="w-full bg-blue-600 text-white py-3 rounded-lg hover:bg-blue-700 transition-colors font-medium"
            >
              Sign In
            </button>
            <p className="text-sm text-gray-500 text-center">
              Get your admin token from the server logs
            </p>
          </div>
        </div>
      </div>
    )
  }

  if (statsLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500"></div>
      </div>
    )
  }

  if (statsError) {
    return (
      <div className="space-y-6">
        <div className="flex justify-between items-center">
          <div>
            <h1 className="text-3xl font-bold text-white">Dashboard</h1>
            <p className="text-gray-400 mt-1">Overview of your LLM proxy usage</p>
          </div>
          <button
            onClick={handleLogout}
            className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors"
          >
            Logout
          </button>
        </div>
        <div className="bg-red-900/50 border border-red-700 rounded-lg p-6 text-red-300">
          Failed to load stats: {statsError.message}
        </div>
      </div>
    )
  }

  const statCards = [
    { label: 'Requests (24h)', value: stats?.total_requests_last_24h || 0, icon: '📊' },
    { label: 'Requests (7d)', value: stats?.total_requests_last_7d || 0, icon: '📈' },
    { label: 'Avg Latency', value: `${stats?.avg_latency_ms || 0}ms`, icon: '⚡' },
    { label: 'Error Rate', value: `${((stats?.error_rate || 0) * 100).toFixed(1)}%`, icon: '❌' },
    { label: 'Total Tokens', value: (stats?.total_tokens_prompt || 0) + (stats?.total_tokens_completion || 0), icon: '🎯' },
  ]

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold text-white">Dashboard</h1>
          <p className="text-gray-400 mt-1">Overview of your LLM proxy usage</p>
        </div>
        <button
          onClick={handleLogout}
          className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors"
        >
          Logout
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
        {statCards.map((card, index) => (
          <div key={index} className="bg-gray-800 border border-gray-700 rounded-xl p-5">
            <div className="flex items-center justify-between mb-3">
              <span className="text-2xl">{card.icon}</span>
              <span className="text-xs text-gray-500 uppercase tracking-wide">{card.label}</span>
            </div>
            <p className="text-2xl font-bold text-white">{card.value}</p>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-gray-800 border border-gray-700 rounded-xl p-6">
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

        <div className="bg-gray-800 border border-gray-700 rounded-xl p-6">
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
      </div>
    </div>
  )
}