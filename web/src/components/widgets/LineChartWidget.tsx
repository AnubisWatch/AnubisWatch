import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { WidgetConfig } from '../../api/client'
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer
} from 'recharts'

interface LineChartWidgetProps {
  widget: WidgetConfig
  dashboardId: string
}

export function LineChartWidget({ widget, dashboardId }: LineChartWidgetProps) {
  const [data, setData] = useState<Array<Record<string, unknown>>>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const fetch = async () => {
      setLoading(true)
      setError(null)
      try {
        const result = await api.post<Array<Record<string, unknown>>>(
          `/dashboards/${dashboardId}/query`,
          widget.query
        )
        if (!cancelled) setData(result || [])
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load')
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

  if (error) {
    return <div className="flex flex-col items-center justify-center h-full text-center px-4">
      <p className="text-rose-400 text-sm mb-1">Error</p>
      <p className="text-gray-400 text-xs">{error}</p>
    </div>
  }

  if (data.length === 0) {
    return <div className="flex items-center justify-center h-full text-gray-500 text-sm">No data</div>
  }

  const metricKey = widget.query.aggregation === 'avg' ? 'avg_latency' : 'count'

  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data}>
        <defs>
          <linearGradient id={`grad-${widget.id}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor="#f59e0b" stopOpacity={0.3} />
            <stop offset="95%" stopColor="#f59e0b" stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
        <XAxis dataKey="time" stroke="#6b7280" fontSize={12} tickLine={false} />
        <YAxis stroke="#6b7280" fontSize={12} tickLine={false} axisLine={false} />
        <Tooltip
          contentStyle={{
            backgroundColor: '#1f2937',
            border: '1px solid #374151',
            borderRadius: '8px',
            color: '#fff'
          }}
        />
        <Area
          type="monotone"
          dataKey={metricKey}
          stroke="#f59e0b"
          strokeWidth={2}
          fill={`url(#grad-${widget.id})`}
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}
