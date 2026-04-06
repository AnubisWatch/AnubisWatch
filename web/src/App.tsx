import { Routes, Route } from 'react-router-dom'
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
import { WebSocketProvider } from './hooks/useWebSocket'

function App() {
  return (
    <WebSocketProvider>
      <Layout>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/souls" element={<Souls />} />
          <Route path="/souls/:id" element={<SoulDetail />} />
          <Route path="/judgments" element={<Judgments />} />
          <Route path="/alerts" element={<Alerts />} />
          <Route path="/journeys" element={<Journeys />} />
          <Route path="/cluster" element={<Cluster />} />
          <Route path="/status-pages" element={<StatusPages />} />
          <Route path="/settings" element={<Settings />} />
        </Routes>
      </Layout>
    </WebSocketProvider>
  )
}

export default App
