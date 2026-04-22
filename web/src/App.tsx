import { useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard } from './pages/Dashboard'
import { Providers } from './pages/Providers'
import { Models } from './pages/Models'
import { Logs } from './pages/Logs'
import { Settings } from './pages/Settings'
import { VirtualKeys } from './pages/VirtualKeys'
import { ThemeProvider } from './context/ThemeContext'
import { ToastProvider } from './context/ToastContext'
import { setAdminToken } from './api/client'

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
      <div className="bg-gray-800 shadow-2xl rounded-2xl p-8 w-full max-w-md border border-gray-700">
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
              className="w-full px-4 py-3 bg-gray-700 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:ring-2 focus:ring-indigo-400 focus:border-transparent outline-none"
              placeholder="Enter your admin token"
              autoFocus={true}
            />
          </div>
          <button
            type="button"
            onClick={handleLogin}
            className="w-full bg-indigo-500 text-white py-3 rounded-lg hover:bg-indigo-600 transition-colors font-medium"
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
      <Routes>
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/providers" element={<Providers />} />
        <Route path="/models" element={<Models />} />
        <Route path="/virtual-keys" element={<VirtualKeys />} />
        <Route path="/logs" element={<Logs />} />
        <Route path="/settings" element={<Settings />} />
      </Routes>
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