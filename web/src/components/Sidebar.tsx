import { NavLink, useLocation } from 'react-router-dom'
import { LayoutDashboard, Ghost, Scale, Bell, Route, Network, Globe, Settings, LogOut } from 'lucide-react'
import { useMemo } from 'react'
import { useAuth } from '../api/hooks'

const navItems = [
  { path: '/', icon: LayoutDashboard, label: 'Dashboard' },
  { path: '/souls', icon: Ghost, label: 'Souls' },
  { path: '/judgments', icon: Scale, label: 'Judgments' },
  { path: '/alerts', icon: Bell, label: 'Alerts' },
  { path: '/journeys', icon: Route, label: 'Journeys' },
  { path: '/cluster', icon: Network, label: 'Necropolis' },
  { path: '/status-pages', icon: Globe, label: 'Status Pages' },
  { path: '/settings', icon: Settings, label: 'Settings' },
]

// Ancient Egypt themed decorative component
const AnkhDecoration = () => (
  <svg className="w-4 h-4 text-[#D4AF37] opacity-60" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 2C10 2 8.5 3.5 8.5 5.5C8.5 6.5 9 7.5 10 8.2V22H14V8.2C15 7.5 15.5 6.5 15.5 5.5C15.5 3.5 14 2 12 2ZM8 12V10H16V12H8Z"/>
  </svg>
)

const LotusDecoration = () => (
  <svg className="w-5 h-5 text-[#40E0D0] opacity-40" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 22C12 22 20 16 20 10C20 6 17 3 12 3C7 3 4 6 4 10C4 16 12 22 12 22ZM12 5C15 5 17 7 17 10C17 14 12 18 12 18C12 18 7 14 7 10C7 7 9 5 12 5Z"/>
  </svg>
)

export function Sidebar() {
  const location = useLocation()
  const { logout } = useAuth()
  const currentPath = useMemo(() => location.pathname, [location])

  return (
    <aside className="w-72 bg-gradient-to-b from-gray-950 via-[#0a0a15] to-gray-950 border-r border-[#D4AF37]/20 flex flex-col h-screen sticky top-0 backdrop-blur-xl hieroglyph-pattern">
      {/* Ancient Egypt decorative top border */}
      <div className="h-1 bg-gradient-to-r from-transparent via-[#D4AF37] to-transparent" />

      {/* Logo - Ancient Egypt themed */}
      <div className="p-6 border-b border-[#D4AF37]/20">
        <div className="flex items-center gap-4">
          {/* Jackal Logo */}
          <div className="relative w-14 h-14">
            <div className="absolute inset-0 bg-[#D4AF37]/30 rounded-full blur-lg animate-pulse" />
            <div className="relative w-14 h-14 rounded-full bg-gradient-to-br from-[#1a1a2e] to-[#0a0a15] flex items-center justify-center
                          border-2 border-[#D4AF37] shadow-lg shadow-[#D4AF37]/30 glow-gold">
              <img src="/jackal-logo.svg" alt="Anubis" className="w-10 h-10" />
            </div>
            {/* Decorative dots */}
            <div className="absolute -top-1 -right-1 w-3 h-3 rounded-full bg-[#40E0D0] border border-[#1a1a2e]" />
          </div>

          <div>
            <h1 className="text-2xl font-cinzel font-bold gradient-gold-shine tracking-wider">Anubis</h1>
            <div className="flex items-center gap-1">
              <AnkhDecoration />
              <p className="text-xs text-[#D4AF37] font-philosopher tracking-[0.2em] uppercase">Watch</p>
              <AnkhDecoration />
            </div>
          </div>
        </div>

        {/* Tagline */}
        <p className="mt-2 text-xs text-gray-500 font-cormorant italic text-center">
          "The Judgment Never Sleeps"
        </p>
      </div>

      {/* Nav - Ancient Egypt themed */}
      <nav className="flex-1 p-4 space-y-1 overflow-y-auto">
        <div className="flex items-center justify-center gap-2 px-4 py-3">
          <div className="h-px flex-1 bg-gradient-to-r from-transparent to-[#D4AF37]/30" />
          <span className="text-xs font-cinzel text-[#D4AF37]/60 tracking-[0.15em] uppercase">Hall of Ma'at</span>
          <div className="h-px flex-1 bg-gradient-to-l from-transparent to-[#D4AF37]/30" />
        </div>

        {navItems.map((item, index) => {
          const IconComponent = item.icon
          const isActive = currentPath === item.path

          return (
            <NavLink
              key={item.path}
              to={item.path}
              className={`group flex items-center gap-3 px-4 py-3 rounded-lg transition-all duration-300 relative overflow-hidden
                ${isActive
                  ? 'bg-gradient-to-r from-[#D4AF37]/20 via-[#D4AF37]/10 to-transparent text-[#F4D03F] border-l-2 border-[#D4AF37]'
                  : 'text-gray-400 hover:text-[#D4AF37] hover:bg-[#D4AF37]/5 border-l-2 border-transparent'
                }`}
            >
              {/* Hover glow effect */}
              <div className={`absolute inset-0 bg-gradient-to-r from-[#D4AF37]/10 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300`} />

              <div className={`relative z-10 ${isActive ? 'animate-bounce-subtle' : ''}`}>
                <IconComponent className={`w-5 h-5 transition-all duration-300 ${isActive ? 'text-[#F4D03F]' : 'group-hover:text-[#D4AF37]'}`} />
                {isActive && (
                  <div className="absolute -inset-1 bg-[#D4AF37]/30 rounded-full blur-sm" />
                )}
              </div>

              <span className="relative z-10 text-sm font-philosopher font-medium tracking-wide">{item.label}</span>

              {isActive && (
                <div className="ml-auto relative z-10">
                  <div className="w-2 h-2 rounded-full bg-[#40E0D0] animate-pulse shadow-lg shadow-[#40E0D0]/50" />
                </div>
              )}

              {/* Decorative number in Egyptian style */}
              <span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-[#D4AF37]/20 font-cinzel">
                {String(index + 1).padStart(2, '0')}
              </span>
            </NavLink>
          )
        })}
      </nav>

      {/* Decorative divider */}
      <div className="px-6 py-2">
        <div className="flex items-center justify-center gap-3">
          <div className="h-px flex-1 bg-gradient-to-r from-transparent via-[#D4AF37]/40 to-transparent" />
          <LotusDecoration />
          <div className="h-px flex-1 bg-gradient-to-r from-transparent via-[#D4AF37]/40 to-transparent" />
        </div>
      </div>

      {/* Status - Ancient Egypt themed */}
      <div className="p-4 space-y-3">
        <div className="relative p-4 bg-gradient-to-r from-[#1E4D2B]/20 to-[#1E4D2B]/5 rounded-xl border border-[#40E0D0]/20 overflow-hidden">
          {/* Nile wave decoration */}
          <div className="absolute inset-0 nile-wave opacity-30" />

          <div className="relative flex items-center gap-3">
            <div className="relative">
              <div className="w-3 h-3 rounded-full bg-[#40E0D0] animate-pulse shadow-lg shadow-[#40E0D0]/50" />
              <div className="absolute inset-0 w-3 h-3 rounded-full bg-[#40E0D0] animate-ping opacity-75" />
            </div>
            <div className="flex-1">
              <p className="text-sm font-cinzel font-semibold text-white">Ma'at Balanced</p>
              <p className="text-xs text-[#40E0D0] font-cormorant">99.9% uptime</p>
            </div>
          </div>
        </div>

        {/* Logout button */}
        <button
          onClick={logout}
          className="flex items-center gap-3 w-full px-4 py-3 text-gray-400 hover:text-[#D4AF37] hover:bg-[#D4AF37]/10 rounded-xl transition-all group border border-transparent hover:border-[#D4AF37]/20"
        >
          <LogOut className="w-5 h-5 group-hover:rotate-180 transition-transform duration-500" />
          <span className="text-sm font-philosopher font-medium">Leave the Temple</span>
        </button>
      </div>

      {/* Ancient Egypt decorative bottom */}
      <div className="p-3 text-center border-t border-[#D4AF37]/10">
        <p className="text-[10px] text-[#D4AF37]/40 font-cinzel tracking-widest">⚖ 𓃥 𓂀 ⚖</p>
      </div>
    </aside>
  )
}
