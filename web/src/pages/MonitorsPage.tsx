import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type Project, type Agent } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { AgentsSection } from '@/components/shared/AgentsSection'
import { timeAgo } from '@/lib/utils'

const formatInterval = (secs: number | null | undefined) => {
  if (!secs) return null
  if (secs < 60) return `every ${secs}s`
  if (secs < 3600) return `every ${Math.round(secs / 60)}m`
  return `every ${Math.round(secs / 3600)}h`
}

// ---- Create / Edit Monitor form (name + description + working dir only) ----

function MonitorForm({ initial, onSave, onClose }: {
  initial?: Project
  onSave: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [workingDir, setWorkingDir] = useState(initial?.working_dir ?? '')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setSaving(true)
    setError('')
    try {
      if (initial) {
        await api.projects.update(initial.id, {
          name: name.trim(), description, working_dir: workingDir.trim(), kind: 'monitor',
        })
      } else {
        await api.projects.create({
          name: name.trim(), description, working_dir: workingDir.trim(), kind: 'monitor',
        })
      }
      onSave()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="mon-name">Monitor Name</Label>
        <Input id="mon-name" value={name} onChange={e => setName(e.target.value)}
          placeholder="e.g. Jira Queue Monitor" />
      </div>
      <div>
        <Label htmlFor="mon-desc">Description</Label>
        <Textarea id="mon-desc" value={description} onChange={e => setDescription(e.target.value)} rows={2}
          placeholder="What does this monitor watch and do?" />
      </div>
      <div>
        <Label htmlFor="mon-wdir">
          Working Directory <span className="text-slate-500 font-normal">(optional)</span>
        </Label>
        <Input id="mon-wdir" value={workingDir} onChange={e => setWorkingDir(e.target.value)}
          placeholder="/path/to/project" />
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>
          {saving ? 'Saving…' : initial ? 'Save' : 'Create Monitor'}
        </Button>
      </div>
    </div>
  )
}

// ---- Monitor card ----

function MonitorCard({ monitor, agents, allAgents, onDelete, onRefresh }: {
  monitor: Project
  agents: Agent[]
  allAgents: Agent[]
  onDelete: () => void
  onRefresh: () => void
}) {
  const navigate = useNavigate()
  const [showEdit, setShowEdit] = useState(false)

  const hasHeartbeat = agents.some(a => (a.heartbeat_interval ?? 0) > 0)
  const heartbeatAgent = agents.find(a => (a.heartbeat_interval ?? 0) > 0)
  const interval = heartbeatAgent ? formatInterval(heartbeatAgent.heartbeat_interval) : null

  const addAgent = async (agentId: string) => {
    await api.projects.assignAgent(monitor.id, agentId)
    onRefresh()
  }

  const removeAgent = async (agentId: string) => {
    await api.projects.removeAgent(monitor.id, agentId)
    onRefresh()
  }

  return (
    <>
      <Card className="hover:border-slate-700 transition-colors">
        <CardBody className="space-y-3">
          {/* Header row */}
          <div className="flex items-start justify-between gap-4">
            {/* Clickable title area */}
            <div
              className="flex-1 min-w-0 cursor-pointer"
              onClick={() => navigate(`/monitors/${monitor.id}`)}
            >
              <div className="flex items-center gap-3 flex-wrap">
                <span className="text-base">⟳</span>
                <h3 className="font-medium text-white">{monitor.name}</h3>
                <Badge variant={monitor.status === 'active' ? 'success' : 'muted'}>
                  {monitor.status}
                </Badge>
                {agents.length > 0 && !hasHeartbeat && (
                  <Badge variant="warning">no heartbeat agent</Badge>
                )}
                {interval && (
                  <span className="text-xs text-violet-400 font-medium">{interval}</span>
                )}
              </div>
              {monitor.description && (
                <p className="text-sm text-slate-400 line-clamp-1 mt-1 ml-6">
                  {monitor.description}
                </p>
              )}
              <div className="flex items-center gap-3 ml-6 mt-1 text-xs text-slate-500">
                {monitor.working_dir && (
                  <span className="font-mono truncate" title={monitor.working_dir}>
                    📁 {monitor.working_dir.split('/').pop()}
                  </span>
                )}
                <span>Created {timeAgo(monitor.created_at)}</span>
                <span
                  className="text-slate-600 hover:text-slate-400 transition-colors"
                  onClick={e => { e.stopPropagation(); navigate(`/monitors/${monitor.id}`) }}
                >
                  View runs →
                </span>
              </div>
            </div>

            {/* Action buttons */}
            <div className="flex gap-2 flex-shrink-0">
              <Button variant="secondary" size="sm" onClick={() => setShowEdit(true)}>Edit</Button>
              <Button variant="danger" size="sm" onClick={onDelete}>Delete</Button>
            </div>
          </div>

          {/* Agents section — always visible, inline */}
          <div className="border-t border-slate-800 pt-3">
            <p className="text-xs text-slate-500 uppercase tracking-wide mb-2">Agents</p>
            <AgentsSection
              assigned={agents}
              allAgents={allAgents}
              showHeartbeat
              onAdd={addAgent}
              onRemove={removeAgent}
            />
          </div>
        </CardBody>
      </Card>

      {showEdit && (
        <Modal title={`Edit: ${monitor.name}`} onClose={() => setShowEdit(false)}>
          <MonitorForm
            initial={monitor}
            onSave={() => { setShowEdit(false); onRefresh() }}
            onClose={() => setShowEdit(false)}
          />
        </Modal>
      )}
    </>
  )
}

// ---- Page ----

export function MonitorsPage() {
  const [monitors, setMonitors] = useState<Project[]>([])
  const [agentsByMonitor, setAgentsByMonitor] = useState<Record<string, Agent[]>>({})
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)

  const load = useCallback(async () => {
    try {
      const [mons, agents] = await Promise.all([
        api.projects.list('monitor'),
        api.agents.list(),
      ])
      setMonitors(mons)
      setAllAgents(agents)
      const agentMap: Record<string, Agent[]> = {}
      await Promise.all(mons.map(async m => {
        agentMap[m.id] = await api.projects.listAgents(m.id)
      }))
      setAgentsByMonitor(agentMap)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const deleteMonitor = async (id: string, name: string) => {
    if (!confirm(`Delete monitor "${name}" and all its run history?`)) return
    await api.projects.delete(id)
    load()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Monitors</h1>
          <p className="text-slate-400 text-sm mt-1">
            Autonomous agents that wake on a schedule, do their work, and sleep
          </p>
        </div>
        <Button onClick={() => setShowForm(true)}>+ New Monitor</Button>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : monitors.length === 0 ? (
        <EmptyState
          icon="⟳"
          title="No monitors yet"
          description="Create a monitor, assign a heartbeat agent, and it will run automatically on schedule."
          action={<Button onClick={() => setShowForm(true)}>New Monitor</Button>}
        />
      ) : (
        <div className="grid gap-4">
          {monitors.map(m => (
            <MonitorCard
              key={m.id}
              monitor={m}
              agents={agentsByMonitor[m.id] ?? []}
              allAgents={allAgents}
              onDelete={() => deleteMonitor(m.id, m.name)}
              onRefresh={load}
            />
          ))}
        </div>
      )}

      {showForm && (
        <Modal title="New Monitor" onClose={() => setShowForm(false)}>
          <MonitorForm
            onSave={() => { setShowForm(false); load() }}
            onClose={() => setShowForm(false)}
          />
        </Modal>
      )}
    </div>
  )
}
