import { useQuery } from '@tanstack/react-query'
import { api, setAdminToken } from '../api/client'
import { useEffect, useState } from 'react'

export function Dashboard() {
  const [adminToken, setAdminTokenInput] = useState('')

  useEffect(() => {
    const token = localStorage.getItem('adminToken')
    if (token) {
      setAdminToken(token)
    }
  }, [])

  const { data: stats, isLoading: statsLoading } = useQuery({
    queryKey: ['stats'],
    queryFn: () => api.stats.get(),
    enabled: !!localStorage.getItem('adminToken'),
  })

  const handleLogin = () => {
    if (adminToken.trim()) {
      localStorage.setItem('adminToken', adminToken.trim())
      setAdminToken(adminToken.trim())
      window.location.reload()
    }
  }

  const handleLogout = () => {
    localStorage.removeItem('adminToken')
    window.location.reload()
  }

  if (!localStorage.getItem('adminToken')) {
    return (
      <div className="max-w-md mx-auto mt-20">
        <div className="bg-white shadow-lg rounded-lg p-8">
          <h2 className="text-2xl font-bold mb-6 text-center">Admin Login</h2>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">
                Admin Token
              </label>
              <input
                type="text"
                value={adminToken}
                onChange={(e) => setAdminTokenInput(e.target.value)}
                className="w-full px-4 py-3 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                placeholder="Enter your admin token"
              />
            </div>
            <button
              onClick={handleLogin}
              className="w-full bg-blue-600 text-white py-3 rounded-lg hover:bg-blue-700 transition-colors font-medium"
            >
              Login
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
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
      </div>
    )
  }

  const statCards = [
    {
      label: 'Requests (24h)',
      value: stats?.total_requests_last_24h || 0,
      icon: '📊',
    },
    {
      label: 'Requests (7d)',
      value: stats?.total_requests_last_7d || 0,
      icon: '📈',
    },
    {
      label: 'Avg Latency',
      value: `${stats?.avg_latency_ms || 0}ms`,
      icon: '⚡',
    },
    {
      label: 'Error Rate',
      value: `${((stats?.error_rate || 0) * 100).toFixed(1)}%`,
      icon: '❌',
    },
    {
      label: 'Total Tokens',
      value: (stats?.total_tokens_prompt || 0) + (stats?.total_tokens_completion || 0),
      icon: '🎯',
    },
  ]

  return (
    <div className="space-y-8">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Dashboard</h1>
          <p className="text-gray-600 mt-1">Overview of your LLM proxy usage</p>
        </div>
        <button
          onClick={handleLogout}
          className="px-4 py-2 bg-gray-200 text-gray-700 rounded-lg hover:bg-gray-300 transition-colors"
        >
          Logout
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-6">
        {statCards.map((card, index) => (
          <div key={index} className="bg-white rounded-lg shadow p-6">
            <div className="flex items-center justify-between mb-4">
              <span className="text-2xl">{card.icon}</span>
              <span className="text-sm text-gray-500">{card.label}</span>
            </div>
            <p className="text-3xl font-bold text-gray-900">{card.value}</p>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-white rounded-lg shadow p-6">
          <h3 className="text-lg font-semibold mb-4">Usage by Model</h3>
          {stats && Object.keys(stats.by_model).length > 0 ? (
            <div className="space-y-3">
              {Object.entries(stats.by_model)
                .sort(([, a], [, b]) => b - a)
                .slice(0, 5)
                .map(([model, count]) => (
                  <div key={model} className="flex justify-between items-center">
                    <span className="text-gray-700 truncate">{model}</span>
                    <span className="font-semibold">{count}</span>
                  </div>
                ))}
            </div>
          ) : (
            <p className="text-gray-500 text-center py-8">No usage data available</p>
          )}
        </div>

        <div className="bg-white rounded-lg shadow p-6">
          <h3 className="text-lg font-semibold mb-4">Usage by Provider</h3>
          {stats && Object.keys(stats.by_provider).length > 0 ? (
            <div className="space-y-3">
              {Object.entries(stats.by_provider)
                .sort(([, a], [, b]) => b - a)
                .slice(0, 5)
                .map(([provider, count]) => (
                  <div key={provider} className="flex justify-between items-center">
                    <span className="text-gray-700 truncate">{provider}</span>
                    <span className="font-semibold">{count}</span>
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
