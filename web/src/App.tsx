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

function App() {
  return (
    <ThemeProvider>
      <ToastProvider>
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
      </ToastProvider>
    </ThemeProvider>
  )
}

export default App