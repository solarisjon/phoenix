import { useState, useEffect, useCallback } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, type Project, type Agent } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label, Select } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { timeAgo } from '@/lib/utils'

// ---- Create Monitor form ----

function CreateMonitorForm({ agents, onSave, onClose }: {
  agents: Agent[]
  onSave: () => void
  onClose: () => void
}) {
  const heartbeatAgents = agents.filter(a => (a.heartbeat_interval ?? 0) > 0 && a.status === 'active')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [workingDir, setWorkingDir] = useState('')
  const [agentId, setAgentId] = useState(heartbeatAgents[0]?.id ?? '')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    if (!agentId) { setError('Select a heartbeat agent'); return }
    setSaving(true)
    try {
      const proj = await api.projects.create({
        name: name.trim(),
        description,
        working_dir: workingDir.trim(),
        kind: 'monitor',
      })
      await api.projects.assignAgent(proj.id, agentId)
      onSave()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to create monitor')
    } finally {
      setSaving(false)
    }
  }

  const formatInterval = (secs: number | null) => {
    if (!secs) return ''
    if (secs < 60) return `every ${secs}s`
    if (secs < 3600) return `every ${Math.round(secs / 60)}m`
    return `every ${Math.round(secs / 3600)}h`
  }

  return (
    <div className="space-y-4">
      {heartbeatAgents.length === 0 && (
        <div className="bg-amber-900/20 border border-amber-700/40 rounded-lg px-4 py-3 text-sm text-amber-300">
          No heartbeat agents available. Go to{' '}
          <a href="/settings?tab=agents" className="underline hover:text-amber-200">
            Settings → Agents
          </a>{' '}
          and set a heartbeat interval on an agent first.
        </div>
      )}
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
        <Label htmlFor="mon-agent">Heartbeat Agent</Label>
        {heartbeatAgents.length === 0 ? (
          <p className="text-sm text-slate-500 mt-1">No heartbeat agents configured.</p>
        ) : (
          <Select id="mon-agent" value={agentId} onChange={e => setAgentId(e.target.value)}>
            {heartbeatAgents.map(a => (
              <option key={a.id} value={a.id}>
                {a.name} — {formatInterval(a.heartbeat_interval)}
              </option>
            ))}
          </Select>
        )}
      </div>
      <div>
        <Label htmlFor="mon-wdir">Working Directory <span className="text-slate-500 font-normal">(optional)</span></Label>
        <Input id="mon-wdir" value={workingDir} onChange={e => setWorkingDir(e.target.value)}
          placeholder="/path/to/project" />
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving || heartbeatAgents.length === 0}>
          {saving ? 'Creating…' : 'Create Monitor'}
        </Button>
      </div>
    </div>
  )
}

// ---- Monitor card ----

function MonitorCard({ monitor, agents, onDelete }: {
  monitor: Project
  agents: Agent[]
  onDelete: () => void
}) {
  const heartbeatAgent = agents.find(a => (a.heartbeat_interval ?? 0) > 0)

  const formatInterval = (secs: number | null) => {
    if (!secs) return null
    if (secs < 60) return `every ${secs}s`
    if (secs < 3600) return `every ${Math.round(secs / 60)}m`
    return `every ${Math.round(secs / 3600)}h`
  }

  const interval = heartbeatAgent ? formatInterval(heartbeatAgent.heartbeat_interval ?? null) : null

  return (
    <Card className="hover:border-slate-700 transition-colors">
      <CardBody className="flex items-start justify-between gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3 mb-1">
            <span className="text-base">⟳</span>
            <h3 className="font-medium text-white">{monitor.name}</h3>
            <Badge variant={monitor.status === 'active' ? 'success' : 'muted'}>{monitor.status}</Badge>
          </div>
          {monitor.description && (
            <p className="text-sm text-slate-400 line-clamp-1 ml-6">{monitor.description}</p>
          )}
          <div className="flex items-center gap-3 ml-6 mt-1.5 text-xs text-slate-500">
            {heartbeatAgent && (
              <span className="text-slate-400">{heartbeatAgent.name}</span>
            )}
            {interval && (
              <span className="text-violet-400 font-medium">{interval}</span>
            )}
            {monitor.working_dir && (
              <span className="font-mono truncate" title={monitor.working_dir}>
                📁 {monitor.working_dir.split('/').pop()}
              </span>
            )}
            <span>Created {timeAgo(monitor.created_at)}</span>
          </div>
        </div>
        <div className="flex gap-2 flex-shrink-0">
          <Link to={`/monitors/${monitor.id}`}>
            <Button variant="secondary" size="sm">View runs</Button>
          </Link>
          <Button variant="danger" size="sm" onClick={onDelete}>Delete</Button>
        </div>
      </CardBody>
    </Card>
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
      // Load assigned agents for each monitor
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
          <p className="text-slate-400 text-sm mt-1">Autonomous agents that wake on a schedule, do their work, and sleep</p>
        </div>
        <Button onClick={() => setShowForm(true)}>+ New Monitor</Button>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : monitors.length === 0 ? (
        <EmptyState
          icon="⟳"
          title="No monitors yet"
          description="Create a monitor to let a heartbeat agent watch external systems, triage work, and dispatch tasks automatically."
          action={<Button onClick={() => setShowForm(true)}>New Monitor</Button>}
        />
      ) : (
        <div className="grid gap-4">
          {monitors.map(m => (
            <MonitorCard
              key={m.id}
              monitor={m}
              agents={agentsByMonitor[m.id] ?? []}
              onDelete={() => deleteMonitor(m.id, m.name)}
            />
          ))}
        </div>
      )}

      {showForm && (
        <Modal title="New Monitor" onClose={() => setShowForm(false)}>
          <CreateMonitorForm
            agents={allAgents}
            onSave={() => { setShowForm(false); load() }}
            onClose={() => setShowForm(false)}
          />
        </Modal>
      )}
    </div>
  )
}
