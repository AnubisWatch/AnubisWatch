import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { WidgetConfig } from '../../api/client'

interface GaugeWidgetProps {
  widget: WidgetConfig
  dashboardId: string
}

export function GaugeWidget({ widget, dashboardId }: GaugeWidgetProps) {
  const [data, setData] = useState<Record<string, number> | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    const fetch = async () => {
      setLoading(true)
      try {
        const result = await api.post<Record<string, number>>(
          `/dashboards/${dashboardId}/query`,
          widget.query
        )
        if (!cancelled) setData(result)
      } catch {
        // silently handle
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetch()
    return () => { cancelled = true }
  }, [dashboardId, widget.query])

  if (loading) {
    return <div className="flex items-center justify-center h-full"><div className="w-6 h-6 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" /></div>
  }

  const rawValue = data ? data[widget.query.metric] ?? Object.values(data)[0] ?? 0 : 0
  const value = typeof rawValue === 'number' ? rawValue : 0

  // Normalize to 0-100 for display
  const normalized = widget.query.metric === 'uptime' ? Math.min(value, 100) : value
  const angle = (normalized / 100) * 180

  // Determine color based on thresholds or default
  let color = '#10b981'
  if (widget.thresholds) {
    for (const t of widget.thresholds) {
      if (t.op === 'lt' && value < t.value) { color = t.color; break }
      if (t.op === 'gt' && value > t.value) { color = t.color; break }
    }
  } else if (normalized < 50) {
    color = '#f43f5e'
  } else if (normalized < 80) {
    color = '#f59e0b'
  }

  return (
    <div className="flex flex-col items-center justify-center h-full">
      <svg width="120" height="70" viewBox="0 0 120 70">
        {/* Background arc */}
        <path
          d="M 10 60 A 50 50 0 0 1 110 60"
          fill="none"
          stroke="#374151"
          strokeWidth="10"
          strokeLinecap="round"
        />
        {/* Value arc */}
        <path
          d="M 10 60 A 50 50 0 0 1 110 60"
          fill="none"
          stroke={color}
          strokeWidth="10"
          strokeLinecap="round"
          strokeDasharray={`${(angle / 180) * 157} 157`}
          className="transition-all duration-500"
        />
      </svg>
      <p className="text-2xl font-bold text-white mt-1">{normalized.toFixed(1)}%</p>
      <p className="text-xs text-gray-500">{widget.title}</p>
    </div>
  )
}
