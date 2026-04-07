import { Sidebar } from './Sidebar'
import { Header } from './Header'
import { Outlet } from 'react-router-dom'

export function Layout() {
  return (
    <div className="flex min-h-screen bg-gradient-to-br from-[#0a0a15] via-[#0f172a] to-[#0a0a15] relative">
      {/* Ancient Egypt background pattern */}
      <div className="fixed inset-0 hieroglyph-pattern opacity-20 pointer-events-none" />

      {/* Sacred geometry decorations */}
      <div className="fixed inset-0 sacred-geometry pointer-events-none" />

      {/* Golden glow effects */}
      <div className="fixed top-0 left-0 w-96 h-96 bg-[#D4AF37]/5 rounded-full blur-3xl pointer-events-none" />
      <div className="fixed bottom-0 right-0 w-96 h-96 bg-[#40E0D0]/5 rounded-full blur-3xl pointer-events-none" />

      <Sidebar />
      <div className="flex-1 flex flex-col min-h-screen relative z-10">
        <Header />
        <main className="flex-1 p-6 overflow-auto">
          <div className="max-w-7xl mx-auto">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
