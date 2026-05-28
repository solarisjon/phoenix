import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { api, type Project, type Task, type CostsResponse } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { taskStatusVariant, taskStatusLabel, formatCost, timeAgo } from '@/lib/utils'

function StatCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <Card>
      <CardBody className="py-5">
        <p className="text-slate-400 text-xs uppercase tracking-wide mb-1">{label}</p>
        <p className="text-3xl font-bold text-white">{value}</p>
        {sub && <p className="text-xs text-slate-500 mt-1">{sub}</p>}
      </CardBody>
    </Card>
  )
}

export function DashboardPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [recentTasks, setRecentTasks] = useState<Task[]>([])
  const [costs, setCosts] = useState<CostsResponse | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const projs = await api.projects.list()
      setProjects(projs)

      // Load recent tasks across all projects
      const taskLists = await Promise.all(projs.map(p => api.tasks.list(p.id).catch(() => [])))
      const all = taskLists.flat().sort((a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      ).slice(0, 10)
      setRecentTasks(all)

      const c = await api.stats.costs()
      setCosts(c)
    } finally { setLoading(false) }
  }, [])

  useEffect(() => {
    load()
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.status_changed') load()
    })
    return unsub
  }, [load])

  const runningCount = recentTasks.filter(t => t.status === 'running' || t.status === 'queued').length
  const pendingApproval = recentTasks.filter(t => t.status === 'awaiting_approval').length

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <p className="text-slate-400 text-sm mt-1">Your agent orchestration control center</p>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-4 gap-4">
        <StatCard label="Active Projects" value={String(projects.filter(p => p.status === 'active').length)} />
        <StatCard label="Tasks Running" value={String(runningCount)} />
        <StatCard label="Needs Approval" value={String(pendingApproval)}
          sub={pendingApproval > 0 ? 'Check inbox' : undefined} />
        <StatCard label="Total Cost" value={costs ? formatCost(costs.total_cost_usd) : '—'} />
      </div>

      <div className="grid grid-cols-3 gap-6">
        {/* Projects */}
        <div className="col-span-1 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Projects</h2>
            <Link to="/projects" className="text-xs text-violet-400 hover:text-violet-300">View all →</Link>
          </div>
          {loading ? (
            <p className="text-slate-500 text-sm">Loading…</p>
          ) : projects.length === 0 ? (
            <Card>
              <CardBody className="py-8 text-center">
                <p className="text-slate-500 text-sm mb-3">No projects yet</p>
                <Link to="/projects" className="text-violet-400 text-xs hover:underline">Create one →</Link>
              </CardBody>
            </Card>
          ) : projects.slice(0, 5).map(p => (
            <Link key={p.id} to={`/projects/${p.id}`}>
              <Card className="hover:border-slate-700 transition-colors cursor-pointer">
                <CardBody className="py-3">
                  <p className="text-sm font-medium text-white truncate">{p.name}</p>
                  <div className="flex items-center gap-2 mt-1">
                    <Badge variant={p.status === 'active' ? 'success' : 'muted'}>{p.status}</Badge>
                    <span className="text-xs text-slate-600">{timeAgo(p.created_at)}</span>
                  </div>
                </CardBody>
              </Card>
            </Link>
          ))}
        </div>

        {/* Recent Tasks */}
        <div className="col-span-2 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Recent Activity</h2>
          </div>
          {loading ? (
            <p className="text-slate-500 text-sm">Loading…</p>
          ) : recentTasks.length === 0 ? (
            <Card>
              <CardBody className="py-12 text-center">
                <div className="text-3xl mb-3">✦</div>
                <p className="text-white font-medium mb-2">Ready to orchestrate</p>
                <p className="text-slate-400 text-sm mb-4">Configure a provider, create an agent, then start a project.</p>
                <div className="flex gap-3 justify-center">
                  <Link to="/providers" className="bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors">
                    Add Provider
                  </Link>
                  <Link to="/agents" className="bg-slate-800 hover:bg-slate-700 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors">
                    Create Agent
                  </Link>
                </div>
              </CardBody>
            </Card>
          ) : (
            <Card>
              <CardHeader>
                <p className="text-sm font-medium text-slate-300">Task Activity</p>
              </CardHeader>
              <div className="divide-y divide-slate-800">
                {recentTasks.map(t => (
                  <div key={t.id} className="px-5 py-3 flex items-center gap-3">
                    <div className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
                      t.status === 'running' ? 'bg-violet-500 animate-pulse' :
                      t.status === 'completed' ? 'bg-emerald-500' :
                      t.status === 'failed' ? 'bg-red-500' :
                      t.status === 'awaiting_approval' ? 'bg-amber-500' : 'bg-slate-600'
                    }`} />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm text-white truncate">{t.title}</p>
                    </div>
                    <Badge variant={taskStatusVariant(t.status)}>{taskStatusLabel(t.status)}</Badge>
                    {t.cost_usd > 0 && <span className="text-xs text-slate-500 flex-shrink-0">{formatCost(t.cost_usd)}</span>}
                    <span className="text-xs text-slate-600 flex-shrink-0">{timeAgo(t.created_at)}</span>
                  </div>
                ))}
              </div>
            </Card>
          )}
        </div>
      </div>
    </div>
  )
}
