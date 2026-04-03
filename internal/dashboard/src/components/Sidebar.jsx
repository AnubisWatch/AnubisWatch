const SidebarItem = ({ icon, label, active, onClick, badge }) => (
  <button
    onClick={onClick}
    className={`w-full flex items-center gap-3 px-4 py-3 rounded-lg transition-all duration-200 group ${
      active
        ? 'bg-amber-500/10 text-amber-400 border-r-2 border-amber-400'
        : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/50'
    }`}
  >
    <span className="text-lg group-hover:scale-110 transition-transform">{icon}</span>
    <span className="font-medium">{label}</span>
    {badge > 0 && (
      <span className="ml-auto bg-red-500 text-white text-xs font-bold px-2 py-0.5 rounded-full">
        {badge}
      </span>
    )}
  </button>
)

function Sidebar({ activeTab, setActiveTab }) {
  const menuItems = [
    { id: 'dashboard', label: 'Dashboard', icon: '◈' },
    { id: 'souls', label: 'Souls', icon: '𓃥' },
    { id: 'judgments', label: 'Judgments', icon: '⚖' },
    { id: 'alerts', label: 'Alerts', icon: '🔔' },
    { id: 'cluster', label: 'Cluster', icon: '𓂀' },
    { id: 'settings', label: 'Settings', icon: '⚙' },
  ]

  return (
    <aside className="fixed left-0 top-0 h-full w-64 bg-slate-900 border-r border-slate-800 z-50 hidden lg:block">
      {/* Logo */}
      <div className="p-6 border-b border-slate-800">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 bg-gradient-to-br from-amber-400 to-amber-600 rounded-lg flex items-center justify-center text-slate-900 font-bold text-xl">
            𓃥
          </div>
          <div>
            <h1 className="font-display font-bold text-xl text-amber-400 tracking-wide">
              AnubisWatch
            </h1>
            <p className="text-xs text-slate-500">The Judgment Never Sleeps</p>
          </div>
        </div>
      </div>

      {/* Navigation */}
      <nav className="p-4 space-y-1">
        {menuItems.map((item) => (
          <SidebarItem
            key={item.id}
            icon={item.icon}
            label={item.label}
            active={activeTab === item.id}
            onClick={() => setActiveTab(item.id)}
          />
        ))}
      </nav>

      {/* Status */}
      <div className="absolute bottom-0 left-0 right-0 p-4 border-t border-slate-800">
        <div className="flex items-center gap-2 text-sm text-slate-400">
          <span className="w-2 h-2 rounded-full bg-green-500 animate-pulse"></span>
          <span>Connected</span>
        </div>
        <div className="text-xs text-slate-600 mt-1">v0.1.0</div>
      </div>
    </aside>
  )
}

export default Sidebar
