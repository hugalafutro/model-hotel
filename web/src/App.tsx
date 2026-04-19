import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard } from './pages/Dashboard'
import { Providers } from './pages/Providers'
import { Models } from './pages/Models'
import { Logs } from './pages/Logs'

function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/providers" element={<Providers />} />
        <Route path="/models" element={<Models />} />
        <Route path="/logs" element={<Logs />} />
      </Routes>
    </Layout>
  )
}

export default App
