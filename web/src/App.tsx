import { Suspense, lazy, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Providers } from './pages/Providers'
import { Models } from './pages/Models'
import { FailoverGroups } from './pages/FailoverGroups'
import { Logs } from './pages/Logs'
import { Settings } from './pages/Settings'
import { VirtualKeys } from './pages/VirtualKeys'
import { ThemeProvider } from './context/ThemeContext'
import { ToastProvider } from './context/ToastContext'
import { setAdminToken } from './api/client'

const Dashboard = lazy(() => import('./pages/Dashboard').then(m => ({ default: m.Dashboard })))

function LoginScreen() {
  const [token, setToken] = useState('')
  const [error, setError] = useState<string | null>(null)

  const handleLogin = () => {
    if (!token.trim()) {
      setError('Please enter an admin token')
      return
    }
    localStorage.setItem('adminToken', token.trim())
    setAdminToken(token.trim())
    window.location.reload()
  }

  return (
    <div className="min-h-screen bg-gray-900 flex items-center justify-center">
      <div className="bg-gray-800 shadow-2xl ui-card p-8 w-full max-w-md">
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-white mb-2">LLM-Proxy</h1>
          <p className="text-gray-400">Multi-Provider LLM Proxy Dashboard</p>
        </div>

        {error && (
          <div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
            {error}
          </div>
        )}

        <div className="space-y-4">
          <div>
            <label htmlFor="admin-token" className="block text-sm font-medium text-gray-300 mb-2">
              Admin Token
            </label>
            <input
              id="admin-token"
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleLogin()}
              className="ui-input"
              placeholder="Enter your admin token"
              autoFocus={true}
            />
          </div>
          <button
            type="button"
            onClick={handleLogin}
            className="w-full bg-(--accent) text-white py-3 rounded-lg hover:brightness-110 transition-all font-medium"
          >
            Sign In
          </button>
          <p className="text-sm text-gray-500 text-center">
            Get your admin token from the server logs
          </p>
        </div>
      </div>
    </div>
  )
}

function AppContent() {
  const token = localStorage.getItem('adminToken')
  if (token) {
    setAdminToken(token)
  }

  if (!token) {
    return <LoginScreen />
  }

  return (
    <Layout>
      <Suspense fallback={
        <div className="flex items-center justify-center h-64">
          <div className="animate-spin rounded-full h-10 w-10 border-b-2" style={{ borderColor: 'var(--accent)' }}></div>
        </div>
      }>
        <Routes>
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/providers" element={<Providers />} />
          <Route path="/models" element={<Models />} />
          <Route path="/failover" element={<FailoverGroups />} />
          <Route path="/virtual-keys" element={<VirtualKeys />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/settings" element={<Settings />} />
        </Routes>
      </Suspense>
    </Layout>
  )
}

function App() {
  return (
    <ThemeProvider>
      <ToastProvider>
        <AppContent />
      </ToastProvider>
    </ThemeProvider>
  )
}

export default App