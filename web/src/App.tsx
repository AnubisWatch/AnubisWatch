import { Routes, Route, Navigate, Outlet } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard } from './pages/Dashboard'
import { Souls } from './pages/Souls'
import { SoulDetail } from './pages/SoulDetail'
import { Judgments } from './pages/Judgments'
import { Alerts } from './pages/Alerts'
import { Journeys } from './pages/Journeys'
import { Cluster } from './pages/Cluster'
import { StatusPages } from './pages/StatusPages'
import { Settings } from './pages/Settings'
import { Login } from './pages/Login'
import { WebSocketProvider } from './hooks/useWebSocket'
import { useAuth } from './api/hooks'

function ProtectedRoute() {
  const { isAuthenticated, loading } = useAuth()

  if (loading) {
    return (
      <div className="min-h-screen bg-gray-950 flex items-center justify-center">
        <div className="w-8 h-8 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <Outlet />
}

function App() {
  return (
    <WebSocketProvider>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<ProtectedRoute />}>
          <Route element={<Layout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/souls" element={<Souls />} />
            <Route path="/souls/:id" element={<SoulDetail />} />
            <Route path="/judgments" element={<Judgments />} />
            <Route path="/alerts" element={<Alerts />} />
            <Route path="/journeys" element={<Journeys />} />
            <Route path="/cluster" element={<Cluster />} />
            <Route path="/status-pages" element={<StatusPages />} />
            <Route path="/settings" element={<Settings />} />
          </Route>
        </Route>
      </Routes>
    </WebSocketProvider>
  )
}

export default App
