import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState } from 'react'

export function Models() {
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedProvider, setSelectedProvider] = useState<string>('')

  const { data: models, isLoading } = useQuery({
    queryKey: ['models', selectedProvider],
    queryFn: () => api.models.list(selectedProvider || undefined),
  })

  const filteredModels = models?.filter((model) =>
    model.model_id.toLowerCase().includes(searchQuery.toLowerCase()) ||
    model.display_name.toLowerCase().includes(searchQuery.toLowerCase())
  ) || []

  const uniqueProviders = Array.from(
    new Set(models?.map(m => m.provider_id))
  )

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">Models</h1>
        <p className="text-gray-600 mt-1">View and manage discovered LLM models</p>
      </div>

      <div className="flex flex-col md:flex-row gap-4">
        <div className="flex-1">
          <input
            type="text"
            placeholder="Search models..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          />
        </div>
        <div className="md:w-64">
          <select
            value={selectedProvider}
            onChange={(e) => setSelectedProvider(e.target.value)}
            className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          >
            <option value="">All Providers</option>
            {uniqueProviders.map((providerId) => (
              <option key={providerId} value={providerId}>
                {providerId}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="bg-white shadow rounded-lg overflow-hidden">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Model ID
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Display Name
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Provider
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Status
              </th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {filteredModels.length > 0 ? (
              filteredModels.map((model) => (
                <tr key={model.id} className="hover:bg-gray-50">
                  <td className="px-6 py-4 whitespace-nowrap">
                    <div className="text-sm font-medium text-gray-900">{model.model_id}</div>
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap">
                    <div className="text-sm text-gray-900">{model.display_name}</div>
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap">
                    <div className="text-sm text-gray-500 font-mono">{model.provider_id}</div>
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap">
                    <span className={`px-2 py-1 text-xs rounded-full ${
                      model.enabled ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'
                    }`}>
                      {model.enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={4} className="px-6 py-12 text-center text-gray-500">
                  {searchQuery || selectedProvider
                    ? 'No models match your search criteria'
                    : 'No models discovered yet. Configure providers and run discovery.'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {models && models.length > 0 && (
        <div className="text-sm text-gray-500 text-center">
          Showing {filteredModels.length} of {models.length} models
        </div>
      )}
    </div>
  )
}
