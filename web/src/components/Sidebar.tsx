import { NavLink } from 'react-router-dom'
import {
  Activity,
  Ghost,
  Scale,
  Bell,
  Route,
  Network,
  Globe,
  Settings,
  ScaleIcon,
} from 'lucide-react'

const navItems = [
  { path: '/', icon: Activity, label: 'Dashboard' },
  { path: '/souls', icon: Ghost, label: 'Souls' },
  { path: '/judgments', icon: Scale, label: 'Judgments' },
  { path: '/alerts', icon: Bell, label: 'Alerts' },
  { path: '/journeys', icon: Route, label: 'Journeys' },
  { path: '/cluster', icon: Network, label: 'Cluster' },
  { path: '/status-pages', icon: Globe, label: 'Status Pages' },
  { path: '/settings', icon: Settings, label: 'Settings' },
]

export function Sidebar() {
  return (
    <aside className="w-64 bg-bg-card border-r border-bg-hover flex flex-col">
      <div className="p-6 flex items-center gap-3 border-b border-bg-hover">
        <div className="w-10 h-10 bg-gradient-to-br from-primary to-secondary rounded-lg flex items-center justify-center">
          <ScaleIcon className="w-6 h-6 text-white" />
        </div>
        <div>
          <h1 className="text-lg font-bold text-text-primary">AnubisWatch</h1>
          <p className="text-xs text-text-muted">The Judgment Never Sleeps</p>
        </div>
      </div>

      <nav className="flex-1 p-4 space-y-1">
        {navItems.map((item) => (
          <NavLink
            key={item.path}
            to={item.path}
            className={({ isActive }) =>
              `flex items-center gap-3 px-4 py-3 rounded-lg transition-colors ${
                isActive
                  ? 'bg-primary text-white'
                  : 'text-text-secondary hover:bg-bg-hover hover:text-text-primary'
              }`
            }
          >
            <item.icon className="w-5 h-5" />
            <span className="font-medium">{item.label}</span>
          </NavLink>
        ))}
      </nav>

      <div className="p-4 border-t border-bg-hover">
        <div className="bg-bg-hover rounded-lg p-4">
          <div className="flex items-center gap-2 mb-2">
            <div className="w-2 h-2 rounded-full bg-success animate-pulse" />
            <span className="text-sm font-medium text-text-primary">System Healthy</span>
          </div>
          <p className="text-xs text-text-muted">All systems operational</p>
        </div>
      </div>
    </aside>
  )
}
