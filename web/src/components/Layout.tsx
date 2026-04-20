import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'

function formatMB(mb: number): string {
  if (mb < 1) return `${mb.toFixed(1)} MB`
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${Math.round(mb)} MB`
}

function SystemStatus() {
  const { data: stats } = useQuery({
    queryKey: ['system'],
    queryFn: () => api.system.get(),
    refetchInterval: 10000,
    retry: false,
  })

  const appMem = stats?.app?.in_container && stats?.app?.memory_limit_bytes
    ? formatMB(stats.app.memory_current_bytes / 1024 / 1024) + ' / ' + formatMB(stats.app.memory_limit_bytes / 1024 / 1024)
    : stats?.app
      ? formatMB(stats.app.heap_alloc_mb) + ' heap'
      : '-'

  return (
    <div className="space-y-1.5 text-[11px] font-mono">
      <div className="flex justify-between items-center text-gray-500">
        <span>API Status</span>
        <span className="flex items-center text-green-400">
          <span className="w-1.5 h-1.5 bg-green-400 rounded-full mr-1.5" />
          Online
        </span>
      </div>
      {stats?.app && (
        <div className="flex justify-between items-center text-gray-500">
          <span>App</span>
          <span className="text-gray-400">
            {appMem}
            <span className="text-gray-600 mx-1">|</span>
            {stats.app.goroutines} goroutines
          </span>
        </div>
      )}
      {stats?.db && (
        <div className="flex justify-between items-center text-gray-500">
          <span>DB</span>
          <span className="text-gray-400">
            {formatMB(stats.db.size_mb)}
            <span className="text-gray-600 mx-1">|</span>
            {stats.db.connections} conn
            <span className="text-gray-600 mx-1">|</span>
            {stats.db.cache_hit_ratio}%
          </span>
        </div>
      )}
    </div>
  )
}

interface LayoutProps {
  children: React.ReactNode
}

export function Layout({ children }: LayoutProps) {
  const location = useLocation()
  const navigate = useNavigate()

  const navigation = [
    { name: 'Dashboard', href: '/dashboard', icon: '📊' },
    { name: 'Providers', href: '/providers', icon: '🔌' },
    { name: 'Models', href: '/models', icon: '🤖' },
    { name: 'Virtual Keys', href: '/virtual-keys', icon: '🔑' },
    { name: 'Logs', href: '/logs', icon: '📝' },
    { name: 'Settings', href: '/settings', icon: '⚙️' },
  ]

  const isActive = (path: string) => location.pathname === path

  const handleLogout = () => {
    localStorage.removeItem('adminToken')
    navigate('/dashboard')
    window.location.reload()
  }

  return (
    <div className="flex h-screen bg-gray-900">
      <aside className="w-64 bg-gray-800 border-r border-gray-700 flex flex-col shrink-0">
        <div className="p-6 border-b border-gray-700">
          <h1 className="text-xl font-bold text-white">LLM-Proxy</h1>
          <p className="text-sm text-gray-400 mt-1">Multi-Provider LLM Proxy</p>
        </div>
        <nav className="flex-1 p-4 overflow-y-auto">
          <ul className="space-y-1">
            {navigation.map((item) => (
              <li key={item.name}>
                <Link
                  to={item.href}
                  className={`flex items-center px-4 py-3 rounded-lg transition-colors ${
                    isActive(item.href)
                      ? 'bg-blue-600 text-white'
                      : 'text-gray-400 hover:bg-gray-700 hover:text-white'
                  }`}
                >
                  <span className="mr-3 text-lg">{item.icon}</span>
                  {item.name}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
        <div className="p-4 border-t border-gray-700 shrink-0">
          <button
            type="button"
            onClick={handleLogout}
            className="w-full px-4 py-2 mb-3 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors text-sm cursor-pointer"
          >
            Logout
          </button>
          <SystemStatus />
        </div>
      </aside>

      <main className="flex-1 overflow-auto">
        <div className="p-8">
          {children}
        </div>
      </main>
    </div>
  )
}
