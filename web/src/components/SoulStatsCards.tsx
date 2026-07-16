import { Ghost, Play, XCircle, Pause, Server } from 'lucide-react'

interface SoulStats {
  total: number
  active: number
  disabled: number
  issues: number
  types: number
}

export function SoulStatsCards({ stats }: { stats: SoulStats }) {
  const cards = [
    { label: 'Total Essence', value: stats.total, color: 'text-white', bg: 'bg-gray-800', icon: Ghost, iconColor: 'text-gray-400' },
    { label: 'Breathing', value: stats.active, color: 'text-emerald-400', bg: 'bg-emerald-500/10', icon: Play, iconColor: 'text-emerald-400' },
    { label: 'Chaos', value: stats.issues, color: 'text-rose-400', bg: 'bg-rose-500/10', icon: XCircle, iconColor: 'text-rose-400' },
    { label: 'Embalmed', value: stats.disabled, color: 'text-gray-400', bg: 'bg-gray-700', icon: Pause, iconColor: 'text-gray-400' },
    { label: 'Rituals', value: stats.types, color: 'text-amber-400', bg: 'bg-amber-500/10', icon: Server, iconColor: 'text-amber-400' },
  ]

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
      {cards.map(({ label, value, color, bg, icon: Icon, iconColor }) => (
        <div key={label} className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">{label}</p>
              <p className={`text-2xl font-bold ${color} mt-1`}>{value}</p>
            </div>
            <div className={`w-10 h-10 ${bg} rounded-xl flex items-center justify-center`}>
              <Icon className={`w-5 h-5 ${iconColor}`} />
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}
