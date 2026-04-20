import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState } from 'react'
import type { Model, ModelCapabilities } from '../api/types'

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

function CapBadge({ on, label }: { on: boolean; label: string }) {
  if (!on) return null
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-blue-900/40 text-blue-300 border border-blue-700/50 mr-1.5 mb-1.5">
      {label}
    </span>
  )
}

function ModelDetailModal({ model, onClose }: { model: Model; onClose: () => void }) {
  const caps = parseCapabilities(model.capabilities)
  const params = parseParams(model.params)
  const inputMods = (() => { try { return JSON.parse(model.input_modalities) as string[] } catch { return [] } })()
  const outputMods = (() => { try { return JSON.parse(model.output_modalities) as string[] } catch { return [] } })()

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={onClose} onKeyDown={(e) => { if (e.key === 'Escape') onClose() }}>
      <div className="bg-gray-800 border border-gray-700 rounded-2xl p-6 w-full max-w-lg max-h-[85vh] overflow-y-auto" onClick={e => e.stopPropagation()} onKeyDown={undefined}>
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
            <div className="flex flex-wrap">
              <CapBadge on={caps.vision || false} label="Vision" />
              <CapBadge on={caps.reasoning || false} label="Reasoning" />
              <CapBadge on={caps.tool_calling || false} label="Tool Calling" />
              <CapBadge on={caps.structured_output || false} label="Structured Output" />
              <CapBadge on={caps.pdf_upload || false} label="PDF Upload" />
              <CapBadge on={caps.video_input || false} label="Video Input" />
              <CapBadge on={caps.audio_input || false} label="Audio Input" />
              <CapBadge on={caps.streaming || false} label="Streaming" />
              <CapBadge on={caps.parallel_tool_calls || false} label="Parallel Tool Calls" />
            </div>
            {!(caps.vision || caps.reasoning || caps.tool_calling || caps.structured_output || caps.pdf_upload || caps.video_input || caps.audio_input) && (
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
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedProvider, setSelectedProvider] = useState<string>('')
  const [detailModel, setDetailModel] = useState<Model | null>(null)

  const { data: models, isLoading } = useQuery({
    queryKey: ['models', selectedProvider],
    queryFn: () => api.models.list(selectedProvider || undefined),
  })

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const filteredModels = models?.filter((model) =>
    model.model_id.toLowerCase().includes(searchQuery.toLowerCase()) ||
    model.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
    model.display_name?.toLowerCase().includes(searchQuery.toLowerCase())
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
        <table className="min-w-full divide-y divide-gray-700">
          <thead className="bg-gray-750">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">
                Model ID
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">
                Provider
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">
                Last Discovered
              </th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">
                Status
              </th>
              <th className="px-6 py-3 text-right text-xs font-medium text-gray-400 uppercase tracking-wider">
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-700">
            {filteredModels.length > 0 ? (
              filteredModels.map((model) => {
                const caps = parseCapabilities(model.capabilities)
                const shortCaps = []
                if (caps?.vision) shortCaps.push('vision')
                if (caps?.reasoning) shortCaps.push('reasoning')
                if (caps?.tool_calling) shortCaps.push('tools')

                return (
                  <tr key={model.id} className="hover:bg-gray-750">
                    <td className="px-6 py-4">
                      <div>
                        <span className="text-sm font-medium text-white">{model.name || model.model_id}</span>
                        {model.name && model.name !== model.model_id && (
                          <span className="text-xs text-gray-500 ml-2">{model.model_id}</span>
                        )}
                      </div>
                      {shortCaps.length > 0 && (
                        <div className="flex gap-1 mt-1">
                          {shortCaps.map(c => (
                            <span key={c} className="px-1.5 py-0.5 text-[10px] rounded bg-blue-900/30 text-blue-400 border border-blue-800/50">{c}</span>
                          ))}
                        </div>
                      )}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className="text-sm text-gray-300">{model.provider_name}</span>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className="text-sm text-gray-400">{formatRelativeTime(model.last_seen_at)}</span>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className={`px-2 py-1 text-xs rounded-full ${
                        model.enabled ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'
                      }`}>
                        {model.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-right">
                      <button
                        type="button"
                        onClick={() => setDetailModel(model)}
                        className="px-3 py-1 text-sm text-blue-400 hover:bg-blue-900/30 rounded transition-colors"
                      >
                        Details
                      </button>
                    </td>
                  </tr>
                )
              })
            ) : (
              <tr>
                <td colSpan={5} className="px-6 py-12 text-center text-gray-500">
                  {searchQuery || selectedProvider
                    ? 'No models match your search criteria'
                    : 'No models discovered yet. Add a provider and discover models.'}
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

      {detailModel && (
        <ModelDetailModal model={detailModel} onClose={() => setDetailModel(null)} />
      )}
    </div>
  )
}