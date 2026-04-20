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

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const providerMap = new Map(providers?.map(p => [p.id, p.name]) || [])

  const filteredModels = models?.filter((model) =>
    model.model_id.toLowerCase().includes(searchQuery.toLowerCase()) ||
    (model.display_name && model.display_name.toLowerCase().includes(searchQuery.toLowerCase()))
  ) || []

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-white">Models</h1>
        <p className="text-gray-400 mt-1">View and manage discovered LLM models</p>
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
        <table className="min-w-full divide-y divide-gray-700">
          <thead className="bg-gray-750">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">
                Model ID
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">
                Display Name
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">
                Provider
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">
                Status
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-700">
            {filteredModels.length > 0 ? (
              filteredModels.map((model) => (
                <tr key={model.id} className="hover:bg-gray-750">
                  <td className="px-6 py-4 whitespace-nowrap">
                    <span className="text-sm font-medium text-white">{model.model_id}</span>
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap">
                    <span className="text-sm text-gray-300">{model.display_name || model.model_id}</span>
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap">
                    <span className="text-sm text-gray-300">{providerMap.get(model.provider_id) || model.provider_id.substring(0, 8)}</span>
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap">
                    <span className={`px-2 py-1 text-xs rounded-full ${
                      model.enabled ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'
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