import { useState } from 'react'
import { Link } from 'react-router-dom'
import { LayoutGrid, Plus, Trash2, BarChart3, Zap, Activity } from 'lucide-react'
import { useDashboards } from '../api/hooks'
import type { CustomDashboard } from '../api/client'

export function Dashboards() {
  const { dashboards, loading, deleteDashboard } = useDashboards()
  const [deleting, setDeleting] = useState<string | null>(null)

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this dashboard?')) return
    setDeleting(id)
    await deleteDashboard(id)
    setDeleting(null)
  }

  const getWidgetIcon = (type: string) => {
    switch (type) {
      case 'line_chart': return Activity
      case 'bar_chart': return BarChart3
      case 'gauge': return Zap
      default: return LayoutGrid
    }
  }

  return (
    <div className="space-y-8 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-cinzel font-bold gradient-gold-shine tracking-wider">Custom Dashboards</h1>
          <p className="text-gray-400 font-cormorant italic mt-1">Build your own views with configurable widgets</p>
        </div>
        <Link
          to="/dashboards/new"
          className="flex items-center gap-2 px-5 py-3 bg-gradient-to-r from-[#B8860B] via-[#D4AF37] to-[#B8860B]
                     hover:from-[#D4AF37] hover:via-[#F4D03F] hover:to-[#D4AF37] text-gray-950 rounded-xl transition-all duration-300
                     font-cinzel font-bold shadow-lg shadow-[#D4AF37]/30 hover:shadow-[#D4AF37]/50 hover:scale-105 active:scale-95"
        >
          <Plus className="w-5 h-5" />
          New Dashboard
        </Link>
      </div>

      {/* Dashboard Grid */}
      {loading ? (
        <div className="flex items-center justify-center h-64" role="status" aria-label="Loading dashboards">
          <div className="w-8 h-8 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
        </div>
      ) : dashboards.length === 0 ? (
        <div className="text-center py-16">
          <div className="w-24 h-24 rounded-2xl bg-gradient-to-br from-amber-500/20 to-amber-600/10
                          flex items-center justify-center border border-amber-500/20 mx-auto mb-6">
            <LayoutGrid className="w-12 h-12 text-amber-400" />
          </div>
          <h3 className="text-2xl font-bold text-white mb-2">No Dashboards Yet</h3>
          <p className="text-gray-400 mb-6 max-w-md mx-auto">
            Create your first custom dashboard to visualize your monitoring data your way.
          </p>
          <Link
            to="/dashboards/new"
            className="inline-flex items-center gap-2 px-6 py-3 bg-gradient-to-r from-amber-600 to-amber-500
                       text-white rounded-xl font-semibold hover:shadow-lg hover:shadow-amber-600/30 transition-all hover:scale-105"
          >
            <Plus className="w-5 h-5" />
            Create Dashboard
          </Link>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {dashboards.map((d: CustomDashboard) => (
            <div
              key={d.id}
              className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6
                         hover:border-amber-500/30 transition-all duration-300 group"
            >
              <div className="flex items-start justify-between mb-4">
                <Link to={`/dashboards/${d.id}`} className="flex-1">
                  <h3 className="text-lg font-semibold text-white group-hover:text-amber-400 transition-colors">{d.name}</h3>
                  {d.description && (
                    <p className="text-sm text-gray-500 mt-1">{d.description}</p>
                  )}
                </Link>
                <button
                  onClick={() => handleDelete(d.id)}
                  disabled={deleting === d.id}
                  className="p-2 text-gray-500 hover:text-red-400 hover:bg-red-500/10 rounded-lg transition-all"
                  aria-label={`Delete dashboard ${d.name}`}
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>

              {/* Widget preview */}
              <div className="flex flex-wrap gap-2 mb-4">
                {d.widgets?.slice(0, 4).map((w) => {
                  const Icon = getWidgetIcon(w.type)
                  return (
                    <span
                      key={w.id}
                      className="flex items-center gap-1 px-2 py-1 bg-gray-800/50 rounded-md text-xs text-gray-400"
                    >
                      <Icon className="w-3 h-3" />
                      {w.title}
                    </span>
                  )
                })}
                {d.widgets?.length > 4 && (
                  <span className="px-2 py-1 bg-gray-800/50 rounded-md text-xs text-gray-500">
                    +{d.widgets.length - 4} more
                  </span>
                )}
              </div>

              <div className="flex items-center justify-between text-xs text-gray-500">
                <span>{d.widgets?.length || 0} widgets</span>
                {d.refresh_sec > 0 && (
                  <span className="flex items-center gap-1">
                    <Activity className="w-3 h-3" />
                    Refreshes every {d.refresh_sec}s
                  </span>
                )}
              </div>

              <Link
                to={`/dashboards/${d.id}`}
                className="mt-4 block text-center text-sm text-amber-400 hover:text-amber-300 font-medium"
              >
                Open Dashboard &rarr;
              </Link>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
