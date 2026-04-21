import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState } from 'react'
import { useToast } from '../context/ToastContext'

export function Providers() {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [showModal, setShowModal] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [discoveringId, setDiscoveringId] = useState<string | null>(null)
  const [formData, setFormData] = useState<{
    name: string;
    base_url: string;
    api_key: string;
  }>({
    name: '',
    base_url: '',
    api_key: '',
  })

  const { data: providers, isLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.settings.get(),
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
      setFormData({ name: '', base_url: '', api_key: '' })
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
    createMutation.mutate(formData)
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
        {providers?.map((provider) => (
          <div key={provider.id} className="bg-gray-800 border border-gray-700 rounded-xl p-6">
            <div className="mb-4">
              <h3 className="text-lg font-semibold text-white">{provider.name}</h3>
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

            <div className="mt-4 flex justify-end gap-2">
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
        ))}

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
                <label htmlFor="provider-name" className="block text-sm font-medium text-gray-300 mb-1">
                  Name
                </label>
                <input
                  id="provider-name"
                  type="text"
                  required
                  autoFocus
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
                    setFormData({ name: '', base_url: '', api_key: '' })
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
    </div>
  )
}