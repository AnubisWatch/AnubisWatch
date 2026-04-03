function RecentJudgments({ judgments }) {
  const getStatusColor = (status) => {
    switch (status) {
      case 'alive':
        return 'text-green-400 bg-green-400/10 border-green-400/20'
      case 'degraded':
        return 'text-amber-400 bg-amber-400/10 border-amber-400/20'
      case 'dead':
        return 'text-red-400 bg-red-400/10 border-red-400/20'
      default:
        return 'text-slate-400 bg-slate-400/10 border-slate-400/20'
    }
  }

  const getStatusIcon = (status) => {
    switch (status) {
      case 'alive':
        return '✓'
      case 'degraded':
        return '⚠'
      case 'dead':
        return '✕'
      default:
        return '?'
    }
  }

  const formatDuration = (ms) => {
    if (ms < 1000) return `${ms}ms`
    return `${(ms / 1000).toFixed(2)}s`
  }

  const formatTime = (timestamp) => {
    if (!timestamp) return '-'
    const date = new Date(timestamp)
    return date.toLocaleTimeString('en-US', { hour12: false })
  }

  return (
    <div className="overflow-x-auto">
      <table className="table-egyptian w-full">
        <thead>
          <tr>
            <th>Status</th>
            <th>Soul</th>
            <th>Duration</th>
            <th>Message</th>
            <th>Time</th>
          </tr>
        </thead>
        <tbody>
          {judgments.length === 0 ? (
            <tr>
              <td colSpan="5" className="text-center py-8 text-slate-500">
                No recent judgments
              </td>
            </tr>
          ) : (
            judgments.map((judgment, i) => (
              <tr key={judgment.id || i} className="animate-slide-in" style={{ animationDelay: `${i * 50}ms` }}>
                <td>
                  <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border ${getStatusColor(judgment.status)}`}>
                    <span>{getStatusIcon(judgment.status)}</span>
                    <span className="capitalize">{judgment.status}</span>
                  </span>
                </td>
                <td className="font-medium">{judgment.soul_name || judgment.soul_id || 'Unknown'}</td>
                <td className="font-mono text-sm text-slate-400">
                  {formatDuration(judgment.duration || 0)}
                </td>
                <td className="max-w-xs truncate text-slate-400">{judgment.message || '-'}</td>
                <td className="text-sm text-slate-500">{formatTime(judgment.timestamp)}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  )
}

export default RecentJudgments
