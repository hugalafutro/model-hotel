import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import {
  Bot,
  Activity,
  TrendingUp,
  Target,
  Clock,
  ArrowUpRight,
  PlugZap,
} from 'lucide-react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
} from 'recharts'

type Range = '24h' | '7d'

function RangeToggle({ value, onChange }: { value: Range; onChange: (v: Range) => void }) {
  return (
    <div className="flex items-center gap-0.5 ml-auto">
      {(['24h', '7d'] as Range[]).map((r) => {
        const active = value === r
        const label = r === '24h' ? '1D' : '7D'
        return (
          <button
            key={r}
            onClick={() => onChange(r)}
            className={`px-2 py-0.5 text-[10px] font-semibold tracking-wide rounded-md transition-colors ${
              active
                ? 'text-white'
                : 'text-(--text-muted) hover:text-(--text-secondary)'
            }`}
            style={active ? { backgroundColor: 'var(--accent)' } : {}}
          >
            {label}
          </button>
        )
      })}
    </div>
  )
}

/* =====================================================
   NUMBER FORMATTERS
   ===================================================== */
function formatCompact(n: number): string {
  if (n === 0) return '0'
  const abs = Math.abs(n)
  if (abs >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (abs >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return n.toFixed(1)
}

/* =====================================================
   ANIMATED COUNTER
   ===================================================== */
function AnimatedValue({ value, decimals = 0, suffix = '', duration = 1200, formatter }: {
  value: number
  decimals?: number
  suffix?: string
  duration?: number
  formatter?: (val: number) => string
}) {
  const [display, setDisplay] = useState(0)
  const startRef = useRef<number | null>(null)
  const fromRef = useRef(0)
  const toRef = useRef(value)

  useEffect(() => {
    fromRef.current = display
    toRef.current = value
    startRef.current = null

    let raf: number
    const ease = (t: number) => 1 - Math.pow(1 - t, 3)

    const tick = (ts: number) => {
      if (startRef.current === null) startRef.current = ts
      const elapsed = ts - startRef.current
      const p = Math.min(elapsed / duration, 1)
      const eased = ease(p)
      const current = fromRef.current + (toRef.current - fromRef.current) * eased
      setDisplay(current)
      if (p < 1) raf = requestAnimationFrame(tick)
    }

    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [value, duration, display])

  const formatted = formatter ? formatter(display) : display.toFixed(decimals)
  return (
    <span style={{ textTransform: 'none' }}>
      {formatted}
      {suffix && (
        <span className="text-sm font-normal text-(--text-muted) ml-1" style={{ textTransform: 'none' }}>
          {suffix}
        </span>
      )}
    </span>
  )
}

/* =====================================================
   STAT CARD
   ===================================================== */
function StatCard({
  label,
  value,
  decimals,
  suffix,
  icon: Icon,
  accent,
  sparkline,
  sparklineTooltip,
  formatter,
}: {
  label: string
  value: number
  decimals?: number
  suffix?: string
  icon: React.ElementType
  accent: string
  sparkline?: number // 0-1 ratio for tiny horizontal fill
  sparklineTooltip?: string
  formatter?: (val: number) => string
}) {
  return (
    <div className="ui-card p-5 group">
      <div className="flex items-center justify-between mb-2">
        <div className="w-9 h-9 flex items-center justify-center rounded-lg" style={{ backgroundColor: `${accent}18` }}>
          <Icon size={18} style={{ color: accent }} />
        </div>
        <span className="text-[10px] font-semibold uppercase tracking-wider text-(--text-muted)">
          {label}
        </span>
      </div>
      <p className="text-xl font-bold text-(--text-primary)" style={{ textTransform: 'none' }}>
        <AnimatedValue value={value} decimals={decimals} suffix={suffix} formatter={formatter} />
      </p>
      {sparkline != null && (
        <div
          className="mt-3 h-1 rounded-full overflow-hidden bg-(--border-subtle)"
          title={sparklineTooltip}
        >
          <div
            className="h-full rounded-full transition-all duration-1000"
            style={{ width: `${Math.max(0, Math.min(1, sparkline)) * 100}%`, backgroundColor: accent }}
          />
        </div>
      )}
    </div>
  )
}

/* =====================================================
   TIME-SERIES AREA CHART
   ===================================================== */
function TimeSeriesChart({ data, range, onRangeChange }: { data: { hour: string; total: number; errors: number }[]; range: Range; onRangeChange: (r: Range) => void }) {
  const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#818cf8'
  const grid = getComputedStyle(document.documentElement).getPropertyValue('--border-subtle').trim() || 'rgba(255,255,255,0.04)'
  const text = getComputedStyle(document.documentElement).getPropertyValue('--text-muted').trim() || '#7a7e8c'

  if (data.length === 0) {
    return (
      <div className="ui-card p-6">
        <h3 className="text-lg font-semibold text-(--text-primary) mb-4 flex items-center gap-2">
          <Activity size={18} className="text-(--accent)" />
          Requests / Hour
        </h3>
        <p className="text-sm text-(--text-muted) text-center py-12">No time-series data yet. Requests will appear here once traffic flows.</p>
      </div>
    )
  }

  return (
    <div className="ui-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
          <Activity size={18} className="text-(--accent)" />
          Requests / {range === '24h' ? 'Hour' : 'Day'}
        </h3>
        <RangeToggle value={range} onChange={onRangeChange} />
      </div>
      <div className="h-60">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data} margin={{ top: 5, right: 5, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id="reqArea" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={accent} stopOpacity={0.3} />
                <stop offset="100%" stopColor={accent} stopOpacity={0.02} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke={grid} vertical={false} />
            <XAxis
              dataKey="hour"
              tick={{ fontSize: 10, fill: text }}
              tickLine={false}
              axisLine={false}
              interval={4}
            />
            <YAxis
              tick={{ fontSize: 10, fill: text }}
              tickLine={false}
              axisLine={false}
              allowDecimals={false}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: 'var(--surface-elevated)',
                border: '1px solid var(--border-default)',
                borderRadius: '10px',
                fontSize: '12px',
              }}
              labelStyle={{ color: 'var(--text-muted)', fontSize: '10px', textTransform: 'uppercase', letterSpacing: '0.05em' }}
              itemStyle={{ color: 'var(--text-primary)', fontSize: '13px' }}
              formatter={(value: number | string | unknown) => [Number(value).toLocaleString(), 'Requests']}
            />
            <Area
              type="monotone"
              dataKey="total"
              stroke={accent}
              strokeWidth={2}
              fill="url(#reqArea)"
              dot={false}
              activeDot={{ r: 4, fill: accent, strokeWidth: 0 }}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}

/* =====================================================
   PROVIDER DOUGHNUT
   ===================================================== */
function ProviderDoughnut({ items, range, onRangeChange }: { items: { name: string; count: number; share: number }[]; range: Range; onRangeChange: (r: Range) => void }) {
  const colors = ['#818cf8', '#059669', '#fbbf24', '#f87171', '#a78bfa']

  if (items.length === 0) {
    return <div className="ui-card p-6 text-center text-sm text-(--text-muted) py-12">No provider data</div>
  }

  return (
    <div className="ui-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
          <TrendingUp size={18} className="text-(--accent)" />
          Provider Breakdown
        </h3>
        <RangeToggle value={range} onChange={onRangeChange} />
      </div>
      <div className="flex items-center gap-6">
        <div className="w-35 h-35">
          <ResponsiveContainer width="100%" height="100%">
            <PieChart>
              <Pie
                data={items}
                cx="50%"
                cy="50%"
                innerRadius={50}
                outerRadius={65}
                paddingAngle={2}
                dataKey="share"
                stroke="none"
              >
                {items.map((_, i) => (
                  <Cell key={i} fill={colors[i % colors.length]} />
                ))}
              </Pie>
            </PieChart>
          </ResponsiveContainer>
        </div>
        <div className="flex-1 space-y-2">
          {items.map((it, i) => (
            <div key={it.name} className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2 min-w-0">
                <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: colors[i % colors.length] }} />
                <span className="text-sm text-(--text-secondary) truncate">{it.name}</span>
              </div>
              <div className="text-right shrink-0">
                <span className="text-sm font-medium text-(--text-primary)">{it.share}%</span>
                <span className="text-xs text-(--text-muted) ml-1">({it.count})</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

/* =====================================================
   TOKEN SPLIT BAR
   ===================================================== */
function TokenSplitBar({ prompt, completion, total, range, onRangeChange }: { prompt: number; completion: number; total: number; range: Range; onRangeChange: (r: Range) => void }) {
  const totalPC = prompt + completion
  if (totalPC === 0) return null
  const promptPct = (prompt / totalPC) * 100
  const completionPct = (completion / totalPC) * 100

  return (
    <div className="ui-card p-6">
      <div className="flex items-center justify-between mb-1">
        <h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
          <Target size={18} className="text-(--accent)" />
          Token Mix
        </h3>
        <RangeToggle value={range} onChange={onRangeChange} />
      </div>
      <p className="text-2xl font-bold text-(--text-primary) mb-4" style={{ textTransform: 'none' }}>
        {total.toLocaleString()} <span className="text-sm font-normal text-(--text-muted)">Tokens</span>
      </p>
      <div className="flex rounded-lg overflow-hidden h-6">
        <div
          className="flex items-center justify-center text-[10px] font-semibold text-white tracking-wider"
          style={{ width: `${promptPct}%`, backgroundColor: '#818cf8' }}
        >
          {promptPct > 12 ? `Prompt ${promptPct.toFixed(0)}%` : ''}
        </div>
        <div
          className="flex items-center justify-center text-[10px] font-semibold text-white tracking-wider"
          style={{ width: `${completionPct}%`, backgroundColor: '#059669' }}
        >
          {completionPct > 12 ? `Completion ${completionPct.toFixed(0)}%` : ''}
        </div>
      </div>
      <div className="flex justify-between mt-3 text-sm">
        <div className="flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-full" style={{ backgroundColor: '#818cf8' }} />
          <span className="text-(--text-tertiary)">Prompt</span>
          <span className="font-medium text-(--text-primary) ml-1">{prompt.toLocaleString()}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-full" style={{ backgroundColor: '#059669' }} />
          <span className="text-(--text-tertiary)">Completion</span>
          <span className="font-medium text-(--text-primary) ml-1">{completion.toLocaleString()}</span>
        </div>
      </div>
    </div>
  )
}

/* =====================================================
   USAGE BAR PANEL
   ===================================================== */
function UsageBarPanel({
  title,
  icon: Icon,
  entries,
  range,
  onRangeChange,
}: {
  title: string
  icon: React.ElementType
  entries: { label: string; value: number; suffix?: string }[]
  range: Range
  onRangeChange: (r: Range) => void
}) {
  const max = entries.length > 0 ? Math.max(...entries.map((e) => e.value)) : 0

  return (
    <div className="ui-card p-6">
      <div className="flex items-center justify-between mb-5">
        <div className="flex items-center gap-2">
          <Icon size={18} className="text-(--accent)" />
          <h3 className="text-lg font-semibold text-(--text-primary)">{title}</h3>
        </div>
        <RangeToggle value={range} onChange={onRangeChange} />
      </div>
      {entries.length === 0 ? (
        <p className="text-sm text-(--text-muted) text-center py-8">No usage data available</p>
      ) : (
        <div className="space-y-3.5">
          {entries.map((entry) => {
            const pct = max > 0 ? (entry.value / max) * 100 : 0
            return (
              <div key={entry.label} className="space-y-1.5">
                <div className="flex justify-between items-center text-sm">
                  <span className="text-(--text-secondary) truncate max-w-[70%]">{entry.label}</span>
                  <span className="font-semibold text-(--text-primary) ml-2 shrink-0">
                    {entry.value.toLocaleString()}{entry.suffix || ''}
                  </span>
                </div>
                <div className="h-1.5 rounded-full overflow-hidden bg-(--border-subtle)">
                  <div
                    className="h-full rounded-full transition-all duration-700"
                    style={{
                      width: `${pct}%`,
                      backgroundColor: 'var(--accent)',
                    }}
                  />
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

/* =====================================================
   GAUGE
   ===================================================== */
function Gauge({ label, value, decimals, suffix, color }: {
  label: string
  value: number
  decimals: number
  suffix: string
  color: string
}) {
  const radius = 40
  const circumference = 2 * Math.PI * radius
  const pathArc = circumference / 2
  const pct = Math.min(Math.max(value, 0), 100)
  const dashOffset = pathArc - (pathArc * pct) / 100

  return (
    <div className="flex flex-col items-center">
      <div className="relative w-28 h-14">
        <svg className="w-full h-full" viewBox="0 0 100 60">
          <path d="M 10 50 A 40 40 0 0 1 90 50" fill="none" stroke="var(--border-subtle)" strokeWidth="8" strokeLinecap="round" />
          <path
            d="M 10 50 A 40 40 0 0 1 90 50"
            fill="none"
            stroke={color}
            strokeWidth="8"
            strokeLinecap="round"
            strokeDasharray={pathArc}
            strokeDashoffset={dashOffset}
            style={{ transition: 'stroke-dashoffset 1s ease-out' }}
          />
        </svg>
        <div className="absolute inset-x-0 bottom-0 text-center">
          <p className="text-sm font-bold text-(--text-primary)">{value.toFixed(decimals)}{suffix}</p>
        </div>
      </div>
      <p className="text-[10px] uppercase tracking-wider text-(--text-muted) mt-2">{label}</p>
    </div>
  )
}

/* =====================================================
   DASHBOARD
   ===================================================== */
export function Dashboard() {
  const [tsRange, setTsRange] = useState<Range>('24h')
  const [provRange, setProvRange] = useState<Range>('24h')
  const [tokenRange, setTokenRange] = useState<Range>('24h')
  const [modelRange, setModelRange] = useState<Range>('24h')
  const [providerRange, setProviderRange] = useState<Range>('24h')
  const [vkRange, setVkRange] = useState<Range>('24h')

  const { data: stats, isLoading: statsLoading, error: statsError } = useQuery({
    queryKey: ['stats', tokenRange],
    queryFn: () => api.stats.get(tokenRange),
    retry: 1,
  })

  const { data: models } = useQuery({
    queryKey: ['models'],
    queryFn: () => api.models.list(),
  })

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.list(),
  })

  const { data: tsData } = useQuery({
    queryKey: ['stats-timeseries', tsRange],
    queryFn: () => api.stats.getTimeSeries(tsRange),
  })

  const { data: provDist } = useQuery({
    queryKey: ['stats-provider-distribution', provRange],
    queryFn: () => api.stats.getProviderDistribution(provRange),
  })

  const { data: modelStats } = useQuery({
    queryKey: ['stats-top-models', modelRange],
    queryFn: () => api.stats.get(modelRange),
  })

  const { data: providerStats } = useQuery({
    queryKey: ['stats-top-providers', providerRange],
    queryFn: () => api.stats.get(providerRange),
  })

  const { data: vkStats } = useQuery({
    queryKey: ['stats-top-virtual-keys', vkRange],
    queryFn: () => api.stats.get(vkRange),
  })

  useQuery({
    queryKey: ['check-auth'],
    queryFn: async () => {
      const response = await fetch('/api/stats', { headers: { Authorization: `Bearer ${localStorage.getItem('adminToken')}` } })
      if (response.status === 401) {
        localStorage.removeItem('adminToken')
        window.location.reload()
      }
      return null
    },
    enabled: !!statsError,
  })

  if (statsError) {
    const errMsg = statsError.message || ''
    if (errMsg.includes('401') || errMsg.includes('Unauthorized') || errMsg.includes('Admin token')) {
      localStorage.removeItem('adminToken')
      window.location.reload()
    }
  }

  if (statsLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2" style={{ borderColor: 'var(--accent)' }}></div>
      </div>
    )
  }

  if (statsError) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold text-white">Dashboard</h1>
          <p className="text-gray-400 mt-1">Overview of your LLM proxy usage</p>
        </div>
        <div className="bg-red-900/50 border border-red-700 rounded-lg p-6 text-red-300">
          Failed to load stats: {statsError.message}
        </div>
      </div>
    )
  }

  // Derived values
  const totalRequests7d = stats?.total_requests_last_7d || 1
  const req24h = stats?.total_requests_last_24h || 0
  const sparkReq = totalRequests7d > 0 ? req24h / totalRequests7d : 0

  const totalTokens = (stats?.total_tokens_prompt || 0) + (stats?.total_tokens_completion || 0)

  const acData = (() => {
    if (!tsData?.points) return []
    return tsData.points.map((p) => {
      const d = new Date(p.bucket)
      const label = tsRange === '7d'
        ? d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
        : d.getHours().toString().padStart(2, '0') + ':00'
      return {
        hour: label,
        total: p.count,
        errors: p.errors,
        tokens: p.tokens,
        latency: Math.round(p.latency_ms),
      }
    })
  })()

  // Format usage panels from their respective range queries
  const byModel = modelStats ? Object.entries(modelStats.by_model).sort(([, a], [, b]) => b - a).slice(0, 5).map(([k, v]) => ({ label: k, value: v })) : []
  const byProvider = providerStats ? Object.entries(providerStats.by_provider).sort(([, a], [, b]) => b - a).slice(0, 5).map(([k, v]) => ({ label: k, value: v })) : []
  const byVK = vkStats ? Object.entries(vkStats.by_virtual_key).sort(([, a], [, b]) => Number(b) - Number(a)).slice(0, 5).map(([k, v]) => ({ label: k, value: Number(v), suffix: ' tokens' })) : []

  // Card accent colors (subtle tints in light, slightly brighter in dark)
  const accents = {
    providers: '#14b8a6',
    models: '#818cf8',
    requests: '#0ea5e9',
    latency: '#f59e0b',
    overhead: '#f472b6',
    errors: '#ef4444',
    tokens: '#22c55e',
  }

  return (
    <div className="space-y-6">
      { /* Page header */ }
      <div className="flex items-end justify-between">
        <div>
          <h1 className="text-3xl font-bold text-(--text-primary)">Dashboard</h1>
          <p className="text-(--text-tertiary) mt-1">Real-time overview of your LLM proxy traffic and performance</p>
        </div>
        <div className="flex gap-4">
          <Gauge label="Avg Overhead" value={((stats?.avg_overhead_ms || 0) / 1000)} decimals={1} suffix="s" color={accents.overhead} />
          <Gauge label="Error Rate" value={((stats?.error_rate || 0) * 100)} decimals={1} suffix="%" color={accents.errors} />
        </div>
      </div>

      { /* Stat cards */ }
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
        <StatCard
          label="Total Providers"
          value={providers?.length || 0}
          icon={PlugZap}
          accent={accents.providers}
        />
        <StatCard
          label="Total Models"
          value={models?.length || 0}
          icon={Bot}
          accent={accents.models}
        />
        <StatCard
          label="Requests (24h)"
          value={req24h}
          icon={Activity}
          accent={accents.requests}
          sparkline={sparkReq}
          sparklineTooltip="Share of last 7 days traffic that was today"
        />
        <StatCard
          label="Requests (7d)"
          value={stats?.total_requests_last_7d || 0}
          icon={TrendingUp}
          accent={accents.requests}
        />
        <StatCard
          label="Avg Duration (24h)"
          value={(stats?.avg_latency_ms || 0) / 1000}
          decimals={1}
          suffix="s"
          icon={Clock}
          accent={accents.latency}
        />
        <StatCard
          label="Avg Tokens (24h)"
          value={stats?.avg_tokens_per_request || 0}
          suffix="T/Rq"
          icon={Target}
          accent={accents.tokens}
          formatter={formatCompact}
        />
      </div>

      { /* Time-series chart — full width */ }
      <TimeSeriesChart data={acData} range={tsRange} onRangeChange={setTsRange} />

      { /* Charts row: doughnut + token split */ }
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <ProviderDoughnut items={provDist?.items || []} range={provRange} onRangeChange={setProvRange} />
        {totalTokens > 0 && (
          <TokenSplitBar
            prompt={stats?.total_tokens_prompt || 0}
            completion={stats?.total_tokens_completion || 0}
            total={totalTokens}
            range={tokenRange}
            onRangeChange={setTokenRange}
          />
        )}
      </div>

      { /* Bottom row: three usage panels with horizontal bars */ }
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <UsageBarPanel title="Top Models" icon={ArrowUpRight} entries={byModel} range={modelRange} onRangeChange={setModelRange} />
        <UsageBarPanel title="Top Providers" icon={ArrowUpRight} entries={byProvider} range={providerRange} onRangeChange={setProviderRange} />
        <UsageBarPanel title="Top Virtual Keys" icon={ArrowUpRight} entries={byVK} range={vkRange} onRangeChange={setVkRange} />
      </div>
    </div>
  )
}
