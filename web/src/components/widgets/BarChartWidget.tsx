import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { WidgetConfig } from '../../api/client'
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell
} from 'recharts'

interface BarChartWidgetProps {
  widget: WidgetConfig
  dashboardId: string
}

export function BarChartWidget({ widget, dashboardId }: BarChartWidgetProps) {
  const [data, setData] = useState<Array<Record<string, unknown>>>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    const fetch = async () => {
      setLoading(true)
      try {
        const result = await api.post<Array<Record<string, unknown>> | Record<string, unknown>>(
          `/dashboards/${dashboardId}/query`,
          widget.query
        )
        if (!cancelled) setData(Array.isArray(result) ? result : result ? [result] : [])
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

  if (data.length === 0) {
    return <div className="flex items-center justify-center h-full text-gray-500 text-sm">No data</div>
  }

  // Check if data has passed/failed (judgment data) or is a simple distribution
  const hasPassFail = data.length > 0 && ('passed' in data[0] || 'failed' in data[0])

  if (hasPassFail) {
    return (
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={data}>
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
          <Bar dataKey="passed" fill="#10b981" radius={[4, 4, 0, 0]} />
          <Bar dataKey="failed" fill="#f43f5e" radius={[4, 4, 0, 0]} />
        </BarChart>
      </ResponsiveContainer>
    )
  }

  // Simple key-value data (e.g., status distribution)
  const entries = Object.entries(data[0] || {})
  return (
    <ResponsiveContainer width="100%" height="100%">
      <BarChart data={entries.map(([key, val]) => ({ name: key, value: val }))}>
        <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
        <XAxis dataKey="name" stroke="#6b7280" fontSize={12} tickLine={false} />
        <YAxis stroke="#6b7280" fontSize={12} tickLine={false} axisLine={false} />
        <Tooltip
          contentStyle={{
            backgroundColor: '#1f2937',
            border: '1px solid #374151',
            borderRadius: '8px',
            color: '#fff'
          }}
        />
        <Bar dataKey="value" radius={[4, 4, 0, 0]}>
          {entries.map((_, i) => (
            <Cell key={i} fill={['#f59e0b', '#10b981', '#f43f5e', '#3b82f6', '#8b5cf6'][i % 5]} />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  )
}
