import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState, useMemo, useCallback } from 'react'
import { useToast } from '../context/ToastContext'
import type { VirtualKey } from '../api/types'

type VKSortField = 'name' | 'key' | 'created' | 'tokens' | 'last_used'
type SortDir = 'asc' | 'desc'

function VKSortHeader({ label, field, sort, onSort }: { label: string; field: VKSortField; sort: { field: VKSortField; dir: SortDir }; onSort: (f: VKSortField) => void }) {
  const active = sort.field === field
  return (
    <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider cursor-pointer select-none hover:text-gray-200 text-gray-400 whitespace-nowrap" onClick={() => onSort(field)}>
      {label} <span className="inline-block w-3 text-center">{active ? (sort.dir === 'asc' ? '↑' : '↓') : ' '}</span>
    </th>
  )
}

function formatRelativeTime(dateStr: string | null): string {
  if (!dateStr) return 'Never'
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

function formatNumber(n: number): string {
  return n.toLocaleString()
}

function CreateKeyModal({ onClose, onToast }: { onClose: () => void; onToast: (msg: string, type: 'success' | 'error' | 'info') => void }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [createdKey, setCreatedKey] = useState<VirtualKey | null>(null)

  const createMutation = useMutation({
    mutationFn: (n: string) => api.virtualKeys.create(n),
    onSuccess: (vk) => {
      setCreatedKey(vk)
      queryClient.invalidateQueries({ queryKey: ['virtualKeys'] })
      onToast('Virtual key created', 'success')
    },
    onError: (err: Error) => {
      onToast(`Failed: ${err.message}`, 'error')
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    createMutation.mutate(name.trim())
  }

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onKeyDown={(e) => { if (e.key === 'Escape') onClose() }}>
      <div className="bg-gray-800 border border-gray-700 rounded-2xl p-6 w-full max-w-md">
        {createdKey ? (
          <>
            <h2 className="text-xl font-bold text-white mb-4">Virtual Key Created</h2>
            <p className="text-sm text-gray-400 mb-3">Copy this key now. It won't be shown again.</p>
            <div className="bg-gray-950 rounded-lg p-3 mb-4 flex items-center justify-between gap-3">
              <code className="text-sm text-green-400 font-mono break-all">{createdKey.key}</code>
              <button
                type="button"
                onClick={() => { navigator.clipboard.writeText(createdKey.key!); onToast('Copied to clipboard', 'info') }}
                className="px-2 py-1 rounded text-xs font-medium border bg-slate-700/40 text-slate-300 border-slate-600/40 hover:brightness-125 transition-all cursor-pointer shrink-0"
              >
                Copy
              </button>
            </div>
            <p className="text-sm text-gray-500 mb-4">Use as: <code className="text-gray-400">Bearer {createdKey.key}</code> at <code className="text-gray-400">{window.location.origin}/v1</code></p>
            <div className="flex justify-end">
              <button
                type="button"
                onClick={onClose}
                className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors cursor-pointer"
              >
                Done
              </button>
            </div>
          </>
        ) : (
          <>
            <h2 className="text-xl font-bold text-white mb-4">Create Virtual Key</h2>
            <form onSubmit={handleSubmit} className="space-y-4">
              <div>
                <label htmlFor="vk-name" className="block text-sm font-medium text-gray-300 mb-1">
                  Name
                </label>
                <input
                  id="vk-name"
                  type="text"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
                  placeholder="e.g., My App"
                />
              </div>
              <div className="flex space-x-3 justify-end pt-2">
                <button
                  type="button"
                  onClick={onClose}
                  className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={createMutation.isPending}
                  className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors disabled:opacity-50 cursor-pointer"
                >
                  {createMutation.isPending ? 'Creating...' : 'Create Key'}
                </button>
              </div>
            </form>
          </>
        )}
      </div>
    </div>
  )
}

function KeyDetailModal({ vk, onClose, onToast }: { vk: VirtualKey; onClose: () => void; onToast: (msg: string, type: 'success' | 'error' | 'info') => void }) {
  const queryClient = useQueryClient()
  const [confirmDelete, setConfirmDelete] = useState(false)

  const deleteMutation = useMutation({
    mutationFn: () => api.virtualKeys.delete(vk.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['virtualKeys'] })
      onToast('Virtual key deleted', 'success')
      onClose()
    },
    onError: (err: Error) => {
      onToast(`Failed to delete: ${err.message}`, 'error')
      setConfirmDelete(false)
    },
  })

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onKeyDown={(e) => { if (e.key === 'Escape') onClose() }}>
      <div className="bg-gray-800 border border-gray-700 rounded-2xl p-6 w-full max-w-md">
        <h2 className="text-xl font-bold text-white mb-4">Virtual Key Details</h2>

        <div className="space-y-3 mb-6">
          <div>
            <span className="text-sm text-gray-500">Name</span>
            <p className="text-gray-200">{vk.name}</p>
          </div>
          <div>
            <span className="text-sm text-gray-500">Key</span>
            <p className="text-gray-200 font-mono">{vk.key_preview}</p>
          </div>
          <div>
            <span className="text-sm text-gray-500">Created</span>
            <p className="text-gray-200">{new Date(vk.created_at).toLocaleString()}</p>
          </div>
          <div>
            <span className="text-sm text-gray-500">Tokens Consumed</span>
            <p className="text-gray-200">{formatNumber(vk.tokens_used)}</p>
          </div>
          <div>
            <span className="text-sm text-gray-500">Last Used</span>
            <p className="text-gray-200">{vk.last_used_at ? new Date(vk.last_used_at).toLocaleString() : 'Never'}</p>
          </div>
        </div>

        <div className="pt-4 border-t border-gray-700 flex justify-between items-center">
          {!confirmDelete ? (
            <button
              type="button"
              onClick={() => setConfirmDelete(true)}
              className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] cursor-pointer transition-all"
            >
              Delete Key
            </button>
          ) : (
            <div className="flex items-center gap-2">
              <span className="text-xs text-red-400">Are you sure?</span>
              <button
                type="button"
                onClick={() => deleteMutation.mutate()}
                disabled={deleteMutation.isPending}
                className="px-3 py-1.5 text-xs rounded-full border bg-red-900/60 text-red-300 border-red-600/60 hover:brightness-125 cursor-pointer transition-all"
              >
                {deleteMutation.isPending ? 'Deleting...' : 'Yes, delete'}
              </button>
              <button
                type="button"
                onClick={() => setConfirmDelete(false)}
                className="px-3 py-1.5 text-xs rounded-full border bg-gray-700/60 text-gray-300 border-gray-600/60 hover:brightness-125 cursor-pointer transition-all"
              >
                Cancel
              </button>
            </div>
          )}
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors cursor-pointer"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

export function VirtualKeys() {
  const { toast } = useToast()
  const [showCreate, setShowCreate] = useState(false)
  const [selectedKey, setSelectedKey] = useState<VirtualKey | null>(null)
  const [sort, setSort] = useState<{ field: VKSortField; dir: SortDir }>({ field: 'name', dir: 'asc' })

  const { data: keys, isLoading } = useQuery({
    queryKey: ['virtualKeys'],
    queryFn: () => api.virtualKeys.list(),
  })

  const handleSort = useCallback((field: VKSortField) => {
    setSort(prev => ({
      field,
      dir: prev.field === field && prev.dir === 'asc' ? 'desc' : 'asc',
    }))
  }, [])

  const sortedKeys = useMemo(() => {
    if (!keys) return []
    const dir = sort.dir === 'asc' ? 1 : -1
    return [...keys].sort((a, b) => {
      switch (sort.field) {
        case 'name': return dir * a.name.localeCompare(b.name)
        case 'created': return dir * (new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
        case 'tokens': return dir * (a.tokens_used - b.tokens_used)
        case 'last_used': {
          const aT = a.last_used_at ? new Date(a.last_used_at).getTime() : 0
          const bT = b.last_used_at ? new Date(b.last_used_at).getTime() : 0
          return dir * (aT - bT)
        }
        default: return 0
      }
    })
  }, [keys, sort])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold text-white">Virtual Keys</h1>
          <p className="text-gray-400 mt-1">Issue keys for clients to access the proxy at /v1</p>
        </div>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors font-medium cursor-pointer"
        >
          + Create Key
        </button>
      </div>

      {sortedKeys.length > 0 ? (
        <div className="border border-gray-700/50 rounded-xl overflow-hidden">
          <table className="w-full table-fixed">
            <colgroup>
              <col className="w-[28%]" />
              <col className="w-[18%]" />
              <col className="w-[22%]" />
              <col className="w-[18%]" />
              <col className="w-[14%]" />
            </colgroup>
            <thead>
              <tr className="bg-gray-800/80">
                <VKSortHeader label="Name" field="name" sort={sort} onSort={handleSort} />
                <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider text-gray-400 whitespace-nowrap">Key <span className="inline-block w-3" /></th>
                <VKSortHeader label="Created" field="created" sort={sort} onSort={handleSort} />
                <VKSortHeader label="Tokens" field="tokens" sort={sort} onSort={handleSort} />
                <VKSortHeader label="Last Used" field="last_used" sort={sort} onSort={handleSort} />
              </tr>
            </thead>
            <tbody>
              {sortedKeys.map((vk, idx) => (
                <tr key={vk.id} className={`${idx % 2 === 1 ? 'bg-white/[0.03]' : ''} hover:bg-gray-700/30 transition-colors`}>
                  <td className="px-4 py-3">
                    <button
                      type="button"
                      onClick={() => setSelectedKey(vk)}
                      className="text-gray-200 hover:text-blue-400 transition-colors cursor-pointer text-sm"
                    >
                      {vk.name}
                    </button>
                  </td>
                  <td className="px-4 py-3">
                    <button
                      type="button"
                      onClick={() => { navigator.clipboard.writeText(vk.key_preview); toast('Key preview copied (full key not available after creation)', 'info') }}
                      className="text-gray-400 font-mono text-xs hover:text-gray-200 transition-colors cursor-pointer"
                    >
                      {vk.key_preview}
                    </button>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-400">{new Date(vk.created_at).toLocaleString()}</td>
                  <td className="px-4 py-3 text-sm text-gray-400 font-mono">{formatNumber(vk.tokens_used)}</td>
                  <td className="px-4 py-3 text-sm text-gray-400">{formatRelativeTime(vk.last_used_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="text-center py-12 bg-gray-800/50 border border-gray-700/50 rounded-xl">
          <p className="text-gray-500">No virtual keys. Create one to start using the proxy.</p>
        </div>
      )}

      {showCreate && (
        <CreateKeyModal
          onClose={() => setShowCreate(false)}
          onToast={toast}
        />
      )}

      {selectedKey && (
        <KeyDetailModal
          vk={selectedKey}
          onClose={() => setSelectedKey(null)}
          onToast={toast}
        />
      )}
    </div>
  )
}
