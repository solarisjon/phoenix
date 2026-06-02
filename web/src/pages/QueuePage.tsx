import { useCallback, useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { Agent, Task } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty'

type ActiveTask = Task & { _agent?: Agent; _projectName?: string }

function sourceIcon(source: string): string {
  if (source === 'monitor') return '⟳'
  if (source === 'agent' || source.startsWith('agent:')) return '✦'
  return '◈' // human
}

function sourceLabel(source: string): string {
  if (source === 'monitor') return 'Monitor'
  if (source === 'agent' || source.startsWith('agent:')) return 'Agent'
  return 'Human'
}

function waitDuration(createdAt: string): string {
  const secs = Math.floor((Date.now() - new Date(createdAt).getTime()) / 1000)
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.floor(secs / 60)}m`
  return `${Math.floor(secs / 3600)}h ${Math.floor((secs % 3600) / 60)}m`
}

export default function QueuePage() {
  const [tasks, setTasks] = useState<ActiveTask[]>([])
  const [agents, setAgents] = useState<Map<string, Agent>>(new Map())
  const [cancelling, setCancelling] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const [running, agentList] = await Promise.all([
        api.tasks.listRunning(),
        api.agents.list(),
      ])
      const agentMap = new Map(agentList.map(a => [a.id, a]))
      setAgents(agentMap)
      const enriched: ActiveTask[] = running.map(t => ({
        ...t,
        _agent: agentMap.get(t.agent_id),
      }))
      // Sort: running first, then queued oldest-first
      enriched.sort((a, b) => {
        if (a.status === 'running' && b.status !== 'running') return -1
        if (b.status === 'running' && a.status !== 'running') return 1
        return new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
      })
      setTasks(enriched)
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => {
    load()
    return phoenixWS.on((ev) => {
      if (ev.type === 'task.status_changed') load()
    })
  }, [load])

  const cancel = async (taskId: string) => {
    setCancelling(prev => new Set(prev).add(taskId))
    try {
      await api.tasks.cancel(taskId)
      await load()
    } catch { /* ignore */ }
    finally {
      setCancelling(prev => { const s = new Set(prev); s.delete(taskId); return s })
    }
  }

  // Group by agent
  const byAgent = new Map<string, ActiveTask[]>()
  for (const t of tasks) {
    const key = t.agent_id
    if (!byAgent.has(key)) byAgent.set(key, [])
    byAgent.get(key)!.push(t)
  }

  const runningCount = tasks.filter(t => t.status === 'running').length
  const queuedCount = tasks.filter(t => t.status === 'queued').length

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-white">Queue</h1>
          <p className="text-sm text-slate-500 mt-0.5">
            {loading ? 'Loading…' : (
              tasks.length === 0
                ? 'No active tasks'
                : `${runningCount} running · ${queuedCount} waiting`
            )}
          </p>
        </div>
        <Button variant="secondary" onClick={load} className="text-xs">↻ Refresh</Button>
      </div>

      {!loading && tasks.length === 0 && (
        <EmptyState
          icon="✦"
          title="Queue is empty"
          description="No tasks are running or waiting. Agents are idle."
        />
      )}

      {/* Per-agent groups */}
      {Array.from(byAgent.entries()).map(([agentId, agentTasks]) => {
        const agent = agents.get(agentId)
        const runningTask = agentTasks.find(t => t.status === 'running')
        const queuedTasks = agentTasks.filter(t => t.status === 'queued')

        return (
          <div key={agentId} className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
            {/* Agent header */}
            <div className="flex items-center gap-3 px-4 py-3 border-b border-slate-800 bg-slate-800/30">
              <span className="text-sm font-medium text-slate-200">{agent?.name ?? agentId.slice(0, 8)}</span>
              <span className="text-xs text-slate-500">{agentTasks.length} task{agentTasks.length !== 1 ? 's' : ''}</span>
              {runningTask && (
                <span className="ml-auto flex items-center gap-1.5 text-xs text-violet-400">
                  <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse" />
                  Running
                </span>
              )}
            </div>

            {/* Task rows */}
            <div className="divide-y divide-slate-800">
              {agentTasks.map((task, idx) => {
                const isRunning = task.status === 'running'
                const queuePos = queuedTasks.indexOf(task)

                return (
                  <div key={task.id} className="flex items-center gap-4 px-4 py-3">
                    {/* Position or running indicator */}
                    <div className="w-8 flex-shrink-0 text-center">
                      {isRunning ? (
                        <span className="w-2 h-2 rounded-full bg-violet-500 animate-pulse inline-block" />
                      ) : (
                        <span className="text-xs text-slate-600 font-mono">#{queuePos + 1}</span>
                      )}
                    </div>

                    {/* Task info */}
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <Badge variant={isRunning ? 'info' : 'muted'}>
                          {isRunning ? 'Running' : 'Queued'}
                        </Badge>
                        <span className="text-sm text-slate-300 truncate">{task.title}</span>
                      </div>
                      <div className="flex items-center gap-3 mt-1 text-xs text-slate-500">
                        <span title="Source">{sourceIcon(task.source)} {sourceLabel(task.source)}</span>
                        <span>waiting {waitDuration(task.created_at)}</span>
                        {task.project_id && (
                          <span className="font-mono text-slate-600">{task.project_id.slice(0, 8)}</span>
                        )}
                      </div>
                    </div>

                    {/* Cancel (queued only) */}
                    {task.status === 'queued' && (
                      <button
                        onClick={() => cancel(task.id)}
                        disabled={cancelling.has(task.id)}
                        className="text-xs text-slate-500 hover:text-red-400 transition-colors flex-shrink-0 px-2 py-1 rounded hover:bg-red-900/20 disabled:opacity-50"
                      >
                        {cancelling.has(task.id) ? 'Cancelling…' : '✕ Remove'}
                      </button>
                    )}
                    {task.status === 'running' && (
                      <button
                        onClick={() => cancel(task.id)}
                        disabled={cancelling.has(task.id)}
                        className="text-xs text-slate-500 hover:text-red-400 transition-colors flex-shrink-0 px-2 py-1 rounded hover:bg-red-900/20 disabled:opacity-50"
                      >
                        {cancelling.has(task.id) ? 'Stopping…' : '⏹ Stop'}
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        )
      })}

      {/* Legend */}
      {tasks.length > 0 && (
        <p className="text-xs text-slate-700 text-center">
          Queue is FIFO per agent · "Remove" cancels before execution · "Stop" interrupts a running task
        </p>
      )}
    </div>
  )
}
