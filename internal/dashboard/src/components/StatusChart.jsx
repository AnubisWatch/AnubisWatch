function StatusChart({ data }) {
  const maxValue = 100
  const minValue = 90

  return (
    <div className="h-48 flex items-end gap-1">
      {data.map((point, i) => {
        const height = ((point.uptime - minValue) / (maxValue - minValue)) * 100
        return (
          <div
            key={i}
            className="flex-1 flex flex-col items-center group"
          >
            <div className="relative w-full">
              <div
                className="w-full bg-gradient-to-t from-amber-500/20 to-amber-400/60 rounded-t transition-all duration-300 group-hover:from-amber-400/30 group-hover:to-amber-300/70"
                style={{ height: `${Math.max(height, 5)}%` }}
              ></div>
              {/* Tooltip */}
              <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 opacity-0 group-hover:opacity-100 transition-opacity bg-slate-800 text-xs px-2 py-1 rounded whitespace-nowrap z-10">
                {point.hour}: {point.uptime.toFixed(1)}%
              </div>
            </div>
            {i % 4 === 0 && (
              <span className="text-xs text-slate-500 mt-1">{point.hour}</span>
            )}
          </div>
        )
      })}
    </div>
  )
}

export default StatusChart
