import { Bell, Search, LogOut, Moon, Sun } from 'lucide-react'
import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../api/hooks'
import { useThemeStore, applyTheme } from '../stores/themeStore'

// Ancient Egypt decorative icons
const ScarabIcon = () => (
  <svg className="w-4 h-4 text-[#D4AF37]" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 2C10 2 8.5 3.5 8.5 5.5C8.5 6.5 9 7.5 10 8.2V9H7C5.5 9 4.5 10 4.5 11.5C4.5 13 5.5 14 7 14H10V15H6C4.5 15 3.5 16 3.5 17.5C3.5 19 4.5 20 6 20H10V21C10 22.1 10.9 23 12 23C13.1 23 14 22.1 14 21V20H18C19.5 20 20.5 19 20.5 17.5C20.5 16 19.5 15 18 15H14V14H17C18.5 14 19.5 13 19.5 11.5C19.5 10 18.5 9 17 9H14V8.2C15 7.5 15.5 6.5 15.5 5.5C15.5 3.5 14 2 12 2Z"/>
  </svg>
)

const EyeOfHorus = () => (
  <svg className="w-5 h-5 text-[#40E0D0]" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 4.5C7 4.5 2.7 7.6 1 12C2.7 16.4 7 19.5 12 19.5C17 19.5 21.3 16.4 23 12C21.3 7.6 17 4.5 12 4.5ZM12 17C8.1 17 4.8 14.9 3.2 12C4.8 9.1 8.1 7 12 7C15.9 7 19.2 9.1 20.8 12C19.2 14.9 15.9 17 12 17ZM12 9.5C10.1 9.5 8.5 11.1 8.5 13C8.5 13.6 8.6 14.2 8.9 14.7L7.5 16.1L9.5 16.5L10.2 18.3L12 17.5C12.3 17.5 12.7 17.5 13 17.5L14.8 18.3L15.5 16.5L17.5 16.1L16.1 14.7C16.4 14.2 16.5 13.6 16.5 13C16.5 11.1 14.9 9.5 13 9.5H12Z"/>
  </svg>
)

export function Header() {
  const [showNotifications, setShowNotifications] = useState(false)
  const [scrolled, setScrolled] = useState(false)
  const { theme, setTheme } = useThemeStore()
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const isDark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)

  useEffect(() => {
    const handleScroll = () => setScrolled(window.scrollY > 10)
    window.addEventListener('scroll', handleScroll)
    return () => window.removeEventListener('scroll', handleScroll)
  }, [])

  const handleLogout = async () => {
    await logout()
    navigate('/login')
  }

  return (
    <header className={`h-16 flex items-center justify-between px-6 sticky top-0 z-50 transition-all duration-300 sacred-geometry ${
      scrolled ? 'bg-gray-950/90 backdrop-blur-xl border-b border-[#D4AF37]/20 shadow-lg shadow-black/20' : 'bg-transparent'
    }`}>
      {/* Search with Egyptian styling */}
      <div className="flex items-center gap-4 flex-1 max-w-md">
        <div className="relative flex-1 group">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 group-focus-within:text-[#D4AF37] transition-colors" />
          <input
            type="text"
            placeholder="Search the archives..."
            className="w-full bg-gray-900/50 border border-gray-800 rounded-xl pl-11 pr-4 py-2.5 text-sm text-white placeholder:text-gray-600
                       focus:outline-none focus:border-[#D4AF37]/50 focus:ring-1 focus:ring-[#D4AF37]/20
                       hover:border-gray-700 transition-all duration-300 font-cormorant"
          />
          <div className="absolute inset-0 rounded-xl bg-gradient-to-r from-[#D4AF37]/0 via-[#D4AF37]/0 to-[#D4AF37]/0
                          group-hover:from-[#D4AF37]/5 group-hover:via-[#D4AF37]/10 group-hover:to-[#D4AF37]/5
                          pointer-events-none transition-all duration-500" />
          {/* Decorative corner */}
          <div className="absolute top-0 right-0 w-2 h-2 border-t border-r border-[#D4AF37]/30 rounded-tr-lg opacity-0 group-hover:opacity-100 transition-opacity" />
        </div>
      </div>

      {/* Center - Ancient Egypt decorative element */}
      <div className="hidden lg:flex items-center gap-4">
        <div className="flex items-center gap-2 px-4 py-1.5 bg-[#D4AF37]/5 rounded-full border border-[#D4AF37]/20">
          <ScarabIcon />
          <span className="text-xs font-cinzel text-[#D4AF37]/80 tracking-wider uppercase">Hall of Ma&apos;at</span>
          <ScarabIcon />
        </div>
      </div>

      {/* Right */}
      <div className="flex items-center gap-3">
        {/* Theme Toggle */}
        <button
          onClick={() => {
            const newTheme = isDark ? 'light' : 'dark'
            setTheme(newTheme)
            applyTheme(newTheme)
          }}
          className="relative p-2.5 text-gray-400 hover:text-[#D4AF37] hover:bg-[#D4AF37]/10 rounded-xl transition-all group border border-transparent hover:border-[#D4AF37]/20"
          aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
        >
          {isDark ? <Sun className="w-5 h-5" /> : <Moon className="w-5 h-5" />}
          <div className="absolute inset-0 rounded-xl bg-[#D4AF37]/10 scale-0 group-hover:scale-100 transition-transform" />
        </button>

        {/* Notifications */}
        <button
          onClick={() => setShowNotifications(!showNotifications)}
          className="relative p-2.5 text-gray-400 hover:text-[#F4D03F] hover:bg-[#D4AF37]/10 rounded-xl transition-all group border border-transparent hover:border-[#D4AF37]/20"
          aria-label="Toggle notifications"
        >
          <Bell className="w-5 h-5 group-hover:animate-swing" />
          <span className="absolute top-1.5 right-1.5 w-2 h-2 bg-[#D4AF37] rounded-full animate-pulse shadow-lg shadow-[#D4AF37]/50" />
          <span className="absolute top-1.5 right-1.5 w-2 h-2 bg-[#D4AF37] rounded-full animate-ping opacity-75" />
        </button>

        {/* User Profile - Egyptian themed */}
        <div className="flex items-center gap-3 pl-4 border-l border-[#D4AF37]/20">
          <div className="text-right hidden sm:block">
            <p className="text-sm font-cinzel font-semibold text-white tracking-wide">{user?.name || 'High Priest'}</p>
            <p className="text-xs text-[#D4AF37]/60 font-cormorant italic">{user?.email || 'priest@anubis.watch'}</p>
          </div>
          <div className="relative group cursor-pointer">
            <div className="w-10 h-10 rounded-full bg-gradient-to-br from-[#D4AF37] to-[#B8860B] flex items-center justify-center
                            shadow-lg shadow-[#D4AF37]/30 group-hover:shadow-[#D4AF37]/50 transition-all border-2 border-[#D4AF37]/50">
              <EyeOfHorus />
            </div>
            <div className="absolute -bottom-1 -right-1 w-3.5 h-3.5 bg-[#40E0D0] rounded-full border-2 border-gray-950 animate-pulse" />
          </div>
          <button
            onClick={handleLogout}
            className="p-2 text-gray-400 hover:text-[#D4AF37] hover:bg-[#D4AF37]/10 rounded-lg transition-all"
            aria-label="Log out"
            title="Log out"
          >
            <LogOut className="w-4 h-4" />
          </button>
        </div>
      </div>
    </header>
  )
}
