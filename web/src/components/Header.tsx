import { Bell, Search, User } from 'lucide-react'
import { useState } from 'react'

export function Header() {
  const [showNotifications, setShowNotifications] = useState(false)

  return (
    <header className="h-16 bg-bg-card border-b border-bg-hover flex items-center justify-between px-6">
      <div className="flex items-center gap-4 flex-1 max-w-xl">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted" />
          <input
            type="text"
            placeholder="Search souls, judgments, alerts..."
            className="w-full bg-bg-dark border border-bg-hover rounded-lg pl-10 pr-4 py-2 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-primary"
          />
        </div>
      </div>

      <div className="flex items-center gap-4">
        <button
          onClick={() => setShowNotifications(!showNotifications)}
          className="relative p-2 text-text-secondary hover:text-text-primary hover:bg-bg-hover rounded-lg transition-colors"
        >
          <Bell className="w-5 h-5" />
          <span className="absolute top-1 right-1 w-2 h-2 bg-error rounded-full" />
        </button>

        <div className="flex items-center gap-3 pl-4 border-l border-bg-hover">
          <div className="text-right">
            <p className="text-sm font-medium text-text-primary">Admin</p>
            <p className="text-xs text-text-muted">admin@anubis.watch</p>
          </div>
          <div className="w-9 h-9 bg-primary rounded-full flex items-center justify-center">
            <User className="w-5 h-5 text-white" />
          </div>
        </div>
      </div>
    </header>
  )
}
