import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState } from 'react'
import { StaticHeaderNoArrow, Row, EmptyRow } from '../components/DataTable'

function formatTPS(t: number | null): string {
  if (t == null) return '-'
  return t.toFixed(1)
}

function formatMs(v: number | null | undefined, decimals: number = 1): string {
  if (v == null || v === 0) return '-'
  return v.toFixed(decimals) + 'ms'
}

interface OverheadBreakdown {
  proxy_overhead_ms: number
  parse_ms: number
  model_lookup_ms: number
  provider_lookup_ms: number
  key_decrypt_ms: number
}

function OverheadModal({ breakdown, onClose }: { breakdown: OverheadBreakdown; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div className="bg-gray-800 border border-gray-700 rounded-xl p-5 min-w-[320px] shadow-2xl" onClick={e => e.stopPropagation()}>
        <div className="flex justify-between items-center mb-4">
          <h3 className="text-lg font-semibold text-white">Proxy Overhead Breakdown</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-white text-xl leading-none">&times;</button>
        </div>
        <div className="space-y-2">
          <div className="flex justify-between text-sm">
            <span className="text-gray-400">Request parsing</span>
            <span className="text-gray-200 font-mono">{formatMs(breakdown.parse_ms)}</span>
          </div>
          <div className="flex justify-between text-sm">
            <span className="text-gray-400">Model lookup</span>
            <span className="text-gray-200 font-mono">{formatMs(breakdown.model_lookup_ms)}</span>
          </div>
          <div className="flex justify-between text-sm">
            <span className="text-gray-400">Provider lookup</span>
            <span className="text-gray-200 font-mono">{formatMs(breakdown.provider_lookup_ms)}</span>
          </div>
          <div className="flex justify-between text-sm">
            <span className="text-gray-400">Key decryption</span>
            <span className="text-gray-200 font-mono">{formatMs(breakdown.key_decrypt_ms)}</span>
          </div>
          <div className="border-t border-gray-700 my-2" />
          <div className="flex justify-between text-sm font-semibold">
            <span className="text-gray-300">Total overhead</span>
            <span className="text-indigo-400 font-mono">{formatMs(breakdown.proxy_overhead_ms)}</span>
          </div>
        </div>
      </div>
    </div>
  )
}

export function Logs() {
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState({ model_id: '', status_code: '' })
  const [overheadBreakdown, setOverheadBreakdown] = useState<OverheadBreakdown | null>(null)

  const { data: logsData, isLoading } = useQuery({
    queryKey: ['logs', page, filters],
    queryFn: () => api.logs.list({
      page,
      per_page: 20,
      model_id: filters.model_id || undefined,
      status_code: filters.status_code ? parseInt(filters.status_code) : undefined,
    }),
  })

  const getStatusBg = (statusCode: number) => {
    if (statusCode >= 200 && statusCode < 300) return 'bg-green-900/30 text-green-400'
    if (statusCode >= 400 && statusCode < 500) return 'bg-yellow-900/30 text-yellow-400'
    if (statusCode >= 500) return 'bg-red-900/30 text-red-400'
    return 'bg-gray-700 text-gray-300'
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
      {overheadBreakdown && (
        <OverheadModal breakdown={overheadBreakdown} onClose={() => setOverheadBreakdown(null)} />
      )}

      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold text-white">Request Logs</h1>
          <p className="text-gray-400 mt-1">View and analyze request history</p>
        </div>
      </div>

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
              <StaticHeaderNoArrow>Provider</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Status</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Tokens</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Duration</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>T/s</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Overhead</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Key</StaticHeaderNoArrow>
            </tr>
          </thead>
          <tbody>
            {logsData?.entries && logsData.entries.length > 0 ? (
              logsData.entries.map((log, idx) => {
                const hasOverhead = log.proxy_overhead_ms != null && log.proxy_overhead_ms > 0
                  && (log.parse_ms > 0 || log.model_lookup_ms > 0 || log.provider_lookup_ms > 0 || log.key_decrypt_ms > 0)
                return (
                  <Row key={log.id} index={idx}>
                    <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400">
                      {log.created_at ? new Date(log.created_at).toLocaleString() : '-'}
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap text-xs font-mono text-gray-400" title={log.request_hash}>
                      {log.request_hash ? log.request_hash.slice(0, 8) : '-'}
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-200 truncate" title={log.model_id}>
                      {log.model_id || '-'}
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-300 truncate">
                      {log.provider_name === 'Deleted' ? <span className="text-red-400 italic">Deleted</span> : (log.provider_name || '-')}
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap">
                      <span className={`px-1.5 py-0.5 text-[10px] rounded-full ${getStatusBg(log.status_code)}`}>
                        {log.status_code}
                      </span>
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                      {log.tokens_prompt + log.tokens_completion > 0
                        ? `${log.tokens_prompt}+${log.tokens_completion}`
                        : '-'}
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                      {log.duration_ms > 0 ? `${(log.duration_ms / 1000).toFixed(1)}s` : '-'}
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                      {formatTPS(log.tokens_per_second)}
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap text-xs font-mono">
                      {log.proxy_overhead_ms != null && log.proxy_overhead_ms > 0 ? (
                        <button
                          className={`${hasOverhead ? 'text-indigo-400 hover:text-indigo-300 cursor-pointer' : 'text-gray-400'}`}
                          onClick={() => hasOverhead ? setOverheadBreakdown({
                            proxy_overhead_ms: log.proxy_overhead_ms,
                            parse_ms: log.parse_ms || 0,
                            model_lookup_ms: log.model_lookup_ms || 0,
                            provider_lookup_ms: log.provider_lookup_ms || 0,
                            key_decrypt_ms: log.key_decrypt_ms || 0,
                          }) : undefined}
                        >
                          {formatMs(log.proxy_overhead_ms)}
                        </button>
                      ) : (
                        <span className="text-gray-400">-</span>
                      )}
                    </td>
                    <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400">
                      {log.virtual_key_deleted
                        ? <span className="text-red-400 italic">Deleted</span>
                        : (log.virtual_key_name || '-')}
                    </td>
                  </Row>
                )
              })
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