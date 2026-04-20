import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useState } from 'react'
import { StaticHeaderNoArrow, Row, EmptyRow } from '../components/DataTable'

export function Logs() {
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState({
    model_id: '',
    status_code: '',
  })

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
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-indigo-400"></div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-white">Request Logs</h1>
        <p className="text-gray-400 mt-1">View and analyze request history</p>
      </div>

      <div className="flex flex-col md:flex-row gap-4">
        <div className="flex-1">
          <input
            type="text"
            placeholder="Filter by model ID..."
            value={filters.model_id}
            onChange={(e) => setFilters({ ...filters, model_id: e.target.value })}
            className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
          />
        </div>
        <div className="md:w-48">
          <select
            value={filters.status_code}
            onChange={(e) => setFilters({ ...filters, status_code: e.target.value })}
            className="w-full px-4 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
          >
            <option value="">All Status Codes</option>
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
              <StaticHeaderNoArrow>Timestamp</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Model</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Status</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Latency</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Tokens</StaticHeaderNoArrow>
              <StaticHeaderNoArrow>Streaming</StaticHeaderNoArrow>
            </tr>
          </thead>
          <tbody>
            {logsData?.entries && logsData.entries.length > 0 ? (
              logsData.entries.map((log, idx) => (
                <Row key={log.id} index={idx}>
                  <td className="px-4 py-3 whitespace-nowrap text-sm text-gray-300">
                    {log.created_at ? new Date(log.created_at).toLocaleString() : '-'}
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap text-sm text-white">{log.model_id || '-'}</td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    <span className={`px-2 py-0.5 text-xs rounded-full ${getStatusBg(log.status_code)}`}>
                      {log.status_code}
                    </span>
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap text-sm text-gray-300">{log.latency_ms}ms</td>
                  <td className="px-4 py-3 whitespace-nowrap text-sm text-gray-300">
                    {log.tokens_prompt + log.tokens_completion > 0
                      ? `${log.tokens_prompt}+${log.tokens_completion}`
                      : '-'}
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    <span className={`px-2 py-0.5 text-xs rounded-full ${
                      log.streaming ? 'bg-indigo-900/30 text-indigo-400' : 'bg-gray-700 text-gray-400'
                    }`}>
                      {log.streaming ? 'Yes' : 'No'}
                    </span>
                  </td>
                </Row>
              ))
            ) : (
              <EmptyRow colSpan={6} message="No logs found" />
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
              className="px-4 py-2 bg-gray-700 border border-gray-600 text-gray-300 rounded-lg hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Previous
            </button>
            <button
              type="button"
              onClick={() => setPage(p => Math.min(Math.ceil(logsData.total / 20), p + 1))}
              disabled={page * 20 >= logsData.total}
              className="px-4 py-2 bg-gray-700 border border-gray-600 text-gray-300 rounded-lg hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  )
}