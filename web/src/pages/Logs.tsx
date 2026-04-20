import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState } from 'react'
import { useToast } from '../context/ToastContext'
import { StaticHeaderNoArrow, Row, EmptyRow } from '../components/DataTable'

function formatTPS(t: number | null): string {
  if (t == null) return '-'
  return t.toFixed(1)
}

export function Logs() {
  const { toast } = useToast()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState({ model_id: '', status_code: '' })
  const [configOpen, setConfigOpen] = useState(false)
  const [retention, setRetention] = useState('')
  const [deleteSelection, setDeleteSelection] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)

  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.settings.get(),
  })

  const currentRetention = settings?.log_retention || 'off'

  const { data: logsData, isLoading } = useQuery({
    queryKey: ['logs', page, filters],
    queryFn: () => api.logs.list({
      page,
      per_page: 20,
      model_id: filters.model_id || undefined,
      status_code: filters.status_code ? parseInt(filters.status_code) : undefined,
    }),
  })

  const purgeMutation = useMutation({
    mutationFn: (olderThan: string) => api.logs.purge(olderThan),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['logs'] })
      toast('Logs deleted', 'success')
      setConfirmDelete(false)
      setDeleteSelection('')
    },
    onError: (err: Error) => {
      toast(`Failed to delete logs: ${err.message}`, 'error')
      setConfirmDelete(false)
    },
  })

  const saveRetention = () => {
    const val = retention || 'off'
    api.settings.update({ log_retention: val === 'off' ? '' : val }).then(() => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      toast(`Log retention set to ${val === 'off' ? 'disabled' : val}`, 'success')
      setRetention('')
    })
  }

  const handleDelete = () => {
    if (!deleteSelection) return
    purgeMutation.mutate(deleteSelection)
  }

  const getStatusBg = (statusCode: number) => {
    if (statusCode >= 200 && statusCode < 300) return 'bg-green-900/30 text-green-400'
    if (statusCode >= 400 && statusCode < 500) return 'bg-yellow-900/30 text-yellow-400'
    if (statusCode >= 500) return 'bg-red-900/30 text-red-400'
    return 'bg-gray-700 text-gray-300'
  }

  const deleteLabels: Record<string, string> = {
    '1h': 'older than 1 hour',
    '1d': 'older than 1 day',
    '1w': 'older than 1 week',
    '1m': 'older than 1 month',
    'all': 'ALL logs',
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-indigo-400" />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold text-white">Request Logs</h1>
          <p className="text-gray-400 mt-1">View and analyze request history</p>
        </div>
        <button
          type="button"
          onClick={() => setConfigOpen(!configOpen)}
          className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors text-sm cursor-pointer"
        >
          {configOpen ? 'Close Config' : 'Config'}
        </button>
      </div>

      {configOpen && (
        <div className="bg-gray-800 border border-gray-700 rounded-xl p-5 space-y-5">
          <div>
            <h3 className="text-sm font-medium text-gray-300 mb-3">Log Retention</h3>
            <p className="text-xs text-gray-500 mb-3">Automatically delete logs older than the selected period. Runs hourly.</p>
            <div className="flex items-center gap-2">
              <select
                value={currentRetention}
                onChange={(e) => setRetention(e.target.value)}
                className="px-3 py-1.5 bg-gray-700 border border-gray-600 rounded-lg text-white text-sm focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
              >
                <option value="off">Disabled</option>
                <option value="1d">1 day</option>
                <option value="1w">1 week</option>
                <option value="1m">1 month (max)</option>
              </select>
              <button
                type="button"
                onClick={saveRetention}
                className="px-3 py-1.5 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors text-sm cursor-pointer"
              >
                Save
              </button>
              {currentRetention && currentRetention !== 'off' && (
                <span className="text-xs text-gray-500">Current: {currentRetention}</span>
              )}
            </div>
          </div>

          <div className="pt-4 border-t border-gray-700">
            <h3 className="text-sm font-medium text-gray-300 mb-3">Delete Logs</h3>
            {!confirmDelete ? (
              <div className="flex items-center gap-2">
                <select
                  value={deleteSelection}
                  onChange={(e) => setDeleteSelection(e.target.value)}
                  className="px-3 py-1.5 bg-gray-700 border border-gray-600 rounded-lg text-white text-sm focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
                >
                  <option value="">Select range...</option>
                  <option value="1h">Older than 1 hour</option>
                  <option value="1d">Older than 1 day</option>
                  <option value="1w">Older than 1 week</option>
                  <option value="1m">Older than 1 month</option>
                  <option value="all">All logs</option>
                </select>
                <button
                  type="button"
                  disabled={!deleteSelection}
                  onClick={() => setConfirmDelete(true)}
                  className="px-3 py-1.5 bg-red-900/50 text-red-400 border border-red-700/50 rounded-lg hover:brightness-125 transition-colors text-sm cursor-pointer disabled:opacity-30 disabled:cursor-not-allowed"
                >
                  Delete
                </button>
              </div>
            ) : (
              <div className="flex items-center gap-3">
                <span className="text-sm text-red-400">Delete {deleteLabels[deleteSelection]}?</span>
                <button
                  type="button"
                  onClick={handleDelete}
                  disabled={purgeMutation.isPending}
                  className="px-3 py-1.5 bg-red-600 text-white rounded-lg hover:bg-red-700 transition-colors text-sm cursor-pointer disabled:opacity-50"
                >
                  {purgeMutation.isPending ? 'Deleting...' : 'Yes, delete'}
                </button>
                <button
                  type="button"
                  onClick={() => { setConfirmDelete(false); setDeleteSelection('') }}
                  className="px-3 py-1.5 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors text-sm cursor-pointer"
                >
                  Cancel
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      <div className="flex flex-col md:flex-row gap-4">
        <div className="flex-1">
          <input
            type="text"
            placeholder="Filter by model ID..."
            value={filters.model_id}
            onChange={(e) => { setFilters({ ...filters, model_id: e.target.value }); setPage(1) }}
            className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
          />
        </div>
        <div className="md:w-48">
          <select
            value={filters.status_code}
            onChange={(e) => { setFilters({ ...filters, status_code: e.target.value }); setPage(1) }}
            className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
          >
            <option value="">All Status</option>
            <option value="200">200 OK</option>
            <option value="400">400 Bad Request</option>
            <option value="401">401 Unauthorized</option>
            <option value="404">404 Not Found</option>
            <option value="500">500 Server Error</option>
          </select>
        </div>
      </div>

      <div className="border border-gray-700/50 rounded-xl overflow-hidden">
        <table className="w-full table-fixed">
          <thead>
            <tr className="bg-gray-800/80">
              <StaticHeaderNoArrow>Time</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Hash</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Model</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Status</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Tokens</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Duration</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>T/s</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Overhead</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Key</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Prompt</StaticHeaderNoArrow>
            </tr>
          </thead>
          <tbody>
            {logsData?.entries && logsData.entries.length > 0 ? (
              logsData.entries.map((log, idx) => (
                <Row key={log.id} index={idx}>
                  <td className="px-3 py-2 whitespace-nowrap text-xs text-gray-400">
                    {log.created_at ? new Date(log.created_at).toLocaleString() : '-'}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-xs font-mono text-gray-400" title={log.request_hash}>
                    {log.request_hash ? log.request_hash.slice(0, 8) : '-'}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-xs text-gray-200 truncate" title={log.model_id}>
                    {log.model_id || '-'}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap">
                    <span className={`px-1.5 py-0.5 text-[10px] rounded-full ${getStatusBg(log.status_code)}`}>
                      {log.status_code}
                    </span>
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                    {log.tokens_prompt + log.tokens_completion > 0
                      ? `${log.tokens_prompt}+${log.tokens_completion}`
                      : '-'}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                    {log.duration_ms > 0 ? `${log.duration_ms}ms` : '-'}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                    {formatTPS(log.tokens_per_second)}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                    {log.proxy_overhead_ms > 0 ? `${log.proxy_overhead_ms}ms` : '-'}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-xs text-gray-400">
                    {log.virtual_key_name || '-'}
                  </td>
                  <td className="px-3 py-2 text-xs text-gray-500 truncate max-w-[120px]" title={log.prompt}>
                    {log.prompt || '-'}
                  </td>
                </Row>
              ))
            ) : (
              <EmptyRow colSpan={10} message="No logs found" />
            )}
          </tbody>
        </table>
      </div>

      {logsData && logsData.total > 0 && (
        <div className="flex items-center justify-between">
          <div className="text-sm text-gray-500">
            Showing {((page - 1) * 20) + 1} to {Math.min(page * 20, logsData.total)} of {logsData.total} entries
          </div>
          <div className="flex space-x-2">
            <button
              type="button"
              onClick={() => setPage(p => Math.max(1, p - 1))}
              disabled={page === 1}
              className="px-4 py-2 bg-gray-700 border border-gray-600 text-gray-300 rounded-lg hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer text-sm"
            >
              Previous
            </button>
            <button
              type="button"
              onClick={() => setPage(p => Math.min(Math.ceil(logsData.total / 20), p + 1))}
              disabled={page * 20 >= logsData.total}
              className="px-4 py-2 bg-gray-700 border border-gray-600 text-gray-300 rounded-lg hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer text-sm"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
