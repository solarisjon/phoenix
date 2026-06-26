import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type CostInsights, type CostInsightsBreakdownRow, type CostInsightsRecommendation } from '@/lib/api'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { formatCost } from '@/lib/utils'

// ---- Helpers ----

function todayStr() {
  return new Date().toISOString().slice(0, 10)
}

function daysAgoStr(n: number) {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d.toISOString().slice(0, 10)
}

function fmt$(n: number) {
  if (n === 0) return '$0.00'
  if (n < 0.01) return '<$0.01'
  return '$' + n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function fmtTokens(n: number) {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(0) + 'K'
  return String(n)
}

// ---- Sub-components ----

function StatCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <Card>
      <CardBody className="py-5">
        <p className="text-slate-400 text-xs uppercase tracking-wide mb-1">{label}</p>
        <p className="text-2xl font-bold text-white">{value}</p>
        {sub && <p className="text-xs mt-1 text-slate-500">{sub}</p>}
      </CardBody>
    </Card>
  )
}

function DateRangePicker({
  from, to, onFromChange, onToChange,
}: {
  from: string; to: string
  onFromChange: (v: string) => void
  onToChange: (v: string) => void
}) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="text-slate-400">From</span>
      <input
        type="date"
        value={from}
        max={to}
        onChange={e => onFromChange(e.target.value)}
        className="bg-slate-800 border border-slate-700 rounded px-2 py-1 text-white text-sm focus:outline-none focus:border-violet-500"
      />
      <span className="text-slate-400">to</span>
      <input
        type="date"
        value={to}
        min={from}
        max={todayStr()}
        onChange={e => onToChange(e.target.value)}
        className="bg-slate-800 border border-slate-700 rounded px-2 py-1 text-white text-sm focus:outline-none focus:border-violet-500"
      />
    </div>
  )
}

type TabKey = 'agent' | 'provider' | 'project'

function CostBreakdownTable({ rows, tab }: { rows: CostInsightsBreakdownRow[]; tab: TabKey }) {
  if (rows.length === 0) {
    return (
      <div className="text-center py-12 text-slate-500 text-sm">
        No cost data for this period.
      </div>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-slate-400 text-xs uppercase tracking-wide border-b border-slate-800">
            <th className="pb-2 pr-4 font-medium">Name</th>
            {tab === 'agent' && <th className="pb-2 pr-4 font-medium">Model</th>}
            {tab === 'agent' && <th className="pb-2 pr-4 font-medium">Provider</th>}
            {tab === 'provider' && <th className="pb-2 pr-4 font-medium">Model</th>}
            <th className="pb-2 pr-4 font-medium text-right">Actual Spend</th>
            <th className="pb-2 pr-4 font-medium text-right">$/Task</th>
            <th className="pb-2 pr-4 font-medium text-right">Proj./mo</th>
            <th className="pb-2 font-medium text-right">Tasks</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-800">
          {rows.map((row, i) => (
            <tr key={row.id || i} className="hover:bg-slate-800/50 transition-colors">
              <td className="py-3 pr-4 font-medium text-white">{row.name}</td>
              {tab === 'agent' && (
                <td className="py-3 pr-4 text-slate-400 font-mono text-xs">{row.model || '—'}</td>
              )}
              {tab === 'agent' && (
                <td className="py-3 pr-4 text-slate-400">{row.provider_name || '—'}</td>
              )}
              {tab === 'provider' && (
                <td className="py-3 pr-4 text-slate-400 font-mono text-xs">{row.model || '—'}</td>
              )}
              <td className="py-3 pr-4 text-right text-white font-medium">{fmt$(row.actual_cost_usd)}</td>
              <td className="py-3 pr-4 text-right text-slate-400">{row.cost_per_task > 0 ? fmt$(row.cost_per_task) : '—'}</td>
              <td className="py-3 pr-4 text-right">
                {row.projected_monthly_usd > 0 ? (
                  <span className={row.projected_monthly_usd > 20 ? 'text-amber-400 font-medium' : 'text-slate-300'}>
                    {fmt$(row.projected_monthly_usd)}
                  </span>
                ) : '—'}
              </td>
              <td className="py-3 text-right text-slate-400">{row.task_count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function RecommendationItem({ rec, onViewAgent }: {
  rec: CostInsightsRecommendation
  onViewAgent: (id: string) => void
}) {
  const isWarning = rec.severity === 'warning'
  return (
    <div className={`flex gap-3 p-4 rounded-lg border ${isWarning ? 'border-amber-500/30 bg-amber-950/20' : 'border-slate-700 bg-slate-800/40'}`}>
      <span className="text-lg flex-shrink-0">{isWarning ? '⚠' : 'ℹ'}</span>
      <div className="flex-1 min-w-0">
        <div className="flex items-start gap-2 flex-wrap">
          <Badge variant={isWarning ? 'warning' : 'muted'} className="text-xs flex-shrink-0">
            {isWarning ? 'warning' : 'info'}
          </Badge>
          <p className="text-white text-sm font-medium">{rec.title}</p>
        </div>
        <p className="text-slate-400 text-sm mt-1">{rec.detail}</p>
        {rec.agent_id && (
          <button
            onClick={() => onViewAgent(rec.agent_id!)}
            className="mt-2 text-violet-400 text-xs hover:text-violet-300 transition-colors"
          >
            View Agent →
          </button>
        )}
      </div>
    </div>
  )
}

// ---- Main Page ----

export default function CostInsightsPage() {
  const navigate = useNavigate()
  const [from, setFrom] = useState(daysAgoStr(30))
  const [to, setTo] = useState(todayStr())
  const [data, setData] = useState<CostInsights | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<TabKey>('agent')

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const d = await api.stats.costInsights(from, to)
      setData(d)
    } catch (e: any) {
      setError(e.message ?? 'Failed to load cost insights')
    } finally {
      setLoading(false)
    }
  }, [from, to])

  useEffect(() => { load() }, [load])

  const tabs: { key: TabKey; label: string }[] = [
    { key: 'agent', label: 'By Agent' },
    { key: 'provider', label: 'By Provider' },
    { key: 'project', label: 'By Project' },
  ]

  const activeRows = data
    ? activeTab === 'agent' ? data.by_agent
    : activeTab === 'provider' ? data.by_provider
    : data.by_project
    : []

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-4">
        <div>
          <h1 className="text-2xl font-bold text-white">Cost Insights</h1>
          <p className="text-slate-400 text-sm mt-0.5">LLM spend breakdown, projections, and optimisation recommendations</p>
        </div>
        <DateRangePicker from={from} to={to} onFromChange={setFrom} onToChange={setTo} />
      </div>

      {error && (
        <div className="bg-red-950/30 border border-red-800 rounded-lg p-4 text-red-300 text-sm">{error}</div>
      )}

      {/* Summary Bar */}
      {loading ? (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          {[0, 1, 2].map(i => (
            <Card key={i}><CardBody className="py-5"><div className="h-8 bg-slate-800 rounded animate-pulse" /></CardBody></Card>
          ))}
        </div>
      ) : data ? (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <StatCard
            label="Total Spend"
            value={fmt$(data.summary.total_actual_usd)}
            sub={`${data.period.from} → ${data.period.to}`}
          />
          <StatCard
            label="Projected / Month"
            value={fmt$(data.summary.projected_monthly_usd)}
            sub="Based on usage rate in this period"
          />
          <StatCard
            label="Tasks Run"
            value={data.summary.task_count.toLocaleString()}
            sub="Completed tasks with cost data"
          />
        </div>
      ) : null}

      {/* Breakdown Tabs */}
      <Card>
        <CardHeader className="border-b border-slate-800 pb-0 pt-4 px-4">
          <div className="flex gap-1">
            {tabs.map(t => (
              <button
                key={t.key}
                onClick={() => setActiveTab(t.key)}
                className={`px-4 py-2 text-sm font-medium rounded-t transition-colors ${
                  activeTab === t.key
                    ? 'bg-slate-800 text-white border-t border-l border-r border-slate-700'
                    : 'text-slate-400 hover:text-white'
                }`}
              >
                {t.label}
              </button>
            ))}
          </div>
        </CardHeader>
        <CardBody className="pt-4">
          {loading ? (
            <div className="space-y-3">
              {[0, 1, 2, 3].map(i => (
                <div key={i} className="h-10 bg-slate-800 rounded animate-pulse" />
              ))}
            </div>
          ) : (
            <CostBreakdownTable rows={activeRows} tab={activeTab} />
          )}
        </CardBody>
      </Card>

      {/* Recommendations */}
      {!loading && data && data.recommendations.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-base font-semibold text-white flex items-center gap-2">
            Recommendations
            <span className="text-xs bg-amber-500/20 text-amber-400 px-2 py-0.5 rounded-full">
              {data.recommendations.length}
            </span>
          </h2>
          {data.recommendations.map((rec, i) => (
            <RecommendationItem
              key={i}
              rec={rec}
              onViewAgent={(id) => navigate(`/agents?highlight=${id}`)}
            />
          ))}
        </div>
      )}

      {!loading && data && data.recommendations.length === 0 && data.summary.task_count > 0 && (
        <Card>
          <CardBody className="py-6 text-center text-slate-500 text-sm">
            ✓ No cost optimisation recommendations for this period.
          </CardBody>
        </Card>
      )}
    </div>
  )
}
