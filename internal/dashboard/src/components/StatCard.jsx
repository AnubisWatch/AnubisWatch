function StatCard({ title, value, icon, color, trend, alert }) {
  const colorClasses = {
    amber: 'text-amber-400 border-amber-400/20 bg-amber-400/5',
    green: 'text-green-400 border-green-400/20 bg-green-400/5',
    yellow: 'text-yellow-400 border-yellow-400/20 bg-yellow-400/5',
    red: 'text-red-400 border-red-400/20 bg-red-400/5',
    blue: 'text-blue-400 border-blue-400/20 bg-blue-400/5',
  }

  return (
    <div className={`card-egyptian p-6 border ${colorClasses[color]} ${alert ? 'animate-pulse border-red-500/50' : ''}`}>
      <div className="flex items-start justify-between">
        <div>
          <p className="text-slate-400 text-sm font-medium">{title}</p>
          <p className="text-3xl font-bold mt-2">{value}</p>
          {trend !== undefined && (
            <p className="text-sm mt-1 text-slate-500">
              {trend}% of total
            </p>
          )}
        </div>
        <div className={`text-3xl ${colorClasses[color].split(' ')[0]}`}>
          {icon}
        </div>
      </div>
    </div>
  )
}

export default StatCard
