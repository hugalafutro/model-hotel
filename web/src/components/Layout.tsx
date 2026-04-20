import { Link, useLocation, useNavigate } from 'react-router-dom'

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
            className="w-full px-4 py-2 mb-3 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors text-sm"
          >
            Logout
          </button>
          <div className="text-sm text-gray-500">
            <div className="flex justify-between items-center">
              <span>API Status</span>
              <span className="flex items-center text-green-400">
                <span className="w-2 h-2 bg-green-400 rounded-full mr-2"></span>
                Online
              </span>
            </div>
          </div>
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