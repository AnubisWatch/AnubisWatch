import { useState, useEffect } from 'react'

function Header({ stats }) {
  const [currentTime, setCurrentTime] = useState(new Date())

  useEffect(() => {
    const timer = setInterval(() => setCurrentTime(new Date()), 1000)
    return () => clearInterval(timer)
  }, [])

  const formatTime = (date) => {
    return date.toLocaleTimeString('en-US', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    })
  }

  return (
    <header className="sticky top-0 z-40 bg-slate-900/80 backdrop-blur-xl border-b border-slate-800">
      <div className="flex items-center justify-between h-16 px-6">
        {/* Left - Mobile Menu Toggle */}
        <div className="flex items-center gap-4 lg:hidden">
          <button className="p-2 text-slate-400 hover:text-amber-400 transition-colors">
            <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </button>
          <span className="font-display font-bold text-amber-400">AnubisWatch</span>
        </div>

        {/* Right - Stats & Actions */}
        <div className="flex items-center gap-6">
          {/* Quick Stats */}
          <div className="hidden md:flex items-center gap-6 text-sm">
            <div className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-green-500"></span>
              <span className="text-slate-400">Healthy:</span>
              <span className="font-semibold text-green-400">{stats.healthy}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-amber-500"></span>
              <span className="text-slate-400">Degraded:</span>
              <span className="font-semibold text-amber-400">{stats.degraded}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-red-500"></span>
              <span className="text-slate-400">Dead:</span>
              <span className="font-semibold text-red-400">{stats.dead}</span>
            </div>
          </div>

          {/* Time */}
          <div className="font-mono text-amber-400 text-lg tracking-wider">
            {formatTime(currentTime)}
          </div>

          {/* User Menu */}
          <button className="flex items-center gap-2 p-2 rounded-lg hover:bg-slate-800 transition-colors">
            <div className="w-8 h-8 bg-gradient-to-br from-amber-400 to-amber-600 rounded-full flex items-center justify-center text-slate-900 font-bold">
              P
            </div>
          </button>
        </div>
      </div>
    </header>
  )
}

export default Header
