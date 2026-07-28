import { useEffect, useState } from 'react'
import { api, type WorkflowRun } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { formatCost, taskStatusLabel, taskStatusVariant } from '@/lib/utils'

function healthBadgeClass(signal: string) {
  if (signal === 'all_clear') return 'bg-emerald-900/40 text-emerald-400 border-emerald-800'
  if (signal === 'needs_attention') return 'bg-amber-900/40 text-amber-400 border-amber-800'
  return 'bg-red-900/40 text-red-400 border-red-800'
}

function healthBadgeLabel(signal: string) {
  if (signal === 'all_clear') return '✓ All clear'
  if (signal === 'needs_attention') return '⚠ Needs attention'
  return '✗ Failed'
}

export function WorkflowRunPanel({ taskId, expanded }: { taskId: string; expanded: boolean }) {
  const [run, setRun] = useState<WorkflowRun | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!expanded) return
    setLoading(true)
    api.tasks.getRun(taskId)
      .then(setRun)
      .catch(() => setRun(null))
      .finally(() => setLoading(false))
  }, [taskId, expanded])

  if (!expanded) return null
  if (loading) return <p className="text-xs text-slate-500 mt-2">Loading run details…</p>
  if (!run) return null

  return (
    <div className="mt-3 space-y-3 border-t border-slate-800 pt-3">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className={`px-2 py-0.5 rounded-full border ${healthBadgeClass(run.derived_health)}`}>
          {healthBadgeLabel(run.derived_health)} (derived)
        </span>
        {run.steps_total > 0 && (
          <span className="text-slate-400">Steps: {run.steps_complete}/{run.steps_total}</span>
        )}
        {run.total_cost > 0 && <span className="text-slate-400">{formatCost(run.total_cost)}</span>}
        {run.duration_sec != null && run.duration_sec > 0 && (
          <span className="text-slate-400">{run.duration_sec}s</span>
        )}
      </div>
      {run.subtasks.length > 0 && (
        <div className="space-y-1">
          <p className="text-[10px] uppercase tracking-wide text-slate-500">Step timeline</p>
          {run.subtasks.map(st => (
            <div key={st.id} className="flex items-center gap-2 text-xs text-slate-300 pl-2 border-l border-slate-700">
              <Badge variant={taskStatusVariant(st.status)} className="text-[10px]">{taskStatusLabel(st.status)}</Badge>
              <span>{st.title}</span>
              {st.step_slug && <span className="text-slate-500 font-mono">{st.step_slug}</span>}
            </div>
          ))}
        </div>
      )}
      {run.deliverables.length > 0 && (
        <div className="space-y-1">
          <p className="text-[10px] uppercase tracking-wide text-slate-500">Deliverables</p>
          {run.deliverables.map(d => (
            <div key={d.path} className="flex items-center gap-2 text-xs">
              <span className={d.verified ? 'text-emerald-400' : 'text-amber-400'}>{d.verified ? '✓' : '○'}</span>
              {d.kind === 'url' ? (
                <a href={d.path} target="_blank" rel="noreferrer" className="text-violet-400 hover:underline truncate">{d.title || d.path}</a>
              ) : (
                <span className="text-slate-300 font-mono truncate">{d.path}</span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export { healthBadgeClass, healthBadgeLabel }
