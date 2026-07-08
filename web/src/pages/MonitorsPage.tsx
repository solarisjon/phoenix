import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type Project, type Agent, type Provider, type ProjectSummary } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label, Select } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { AgentsSection } from '@/components/shared/AgentsSection'
import { TagInput, TagPill } from '@/components/ui/tag-input'
import { FilterSortBar } from '@/components/ui/filter-sort-bar'
import { applyFilterSort, collectAllTags, type FilterSortState } from '@/components/ui/filter-sort-utils'
import {
  ScheduleEditor,
} from '@/components/monitor/ScheduleEditor'
import {
  scheduleFromProject,
  schedulePayload,
  scheduleError,
  scheduleSummary,
  type ScheduleValue,
} from '@/components/monitor/schedule'
import { timeAgo } from '@/lib/utils'
import { WorkingDirInput } from '@/components/ui/working-dir-input'
import { ProviderSelect } from '@/components/ui/provider-select'
import { cn } from '@/lib/utils'
import { getErrorMessage } from '@/lib/errors'

// ---- Create / Edit Monitor form (name + description + working dir only) ----
function MonitorForm({ initial, providers, allTags, onSave, onClose }: {
  initial?: Project
  providers: Provider[]
  allTags: string[]
  onSave: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.objective ?? '')
  const [workingDir, setWorkingDir] = useState(initial?.working_dir ?? '')
  const [tags, setTags] = useState<string[]>(initial?.tags ?? [])
  const [criticMode, setCriticMode] = useState(initial?.critic_mode ?? 'none')
  const [schedule, setSchedule] = useState<ScheduleValue>(scheduleFromProject(initial))
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [showAI, setShowAI] = useState(false)
  const [aiHint, setAiHint] = useState('')
  const [aiProviderID, setAiProviderID] = useState(
    providers.find(p => p.type === 'llm')?.id ?? providers[0]?.id ?? ''
  )
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiError, setAiError] = useState('')

  const save = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    const schedErr = scheduleError(schedule)
    if (schedErr) { setError(schedErr); return }
    // Validate criticMode
    const validCriticModes = ['none', 'builtin']
    if (!validCriticModes.includes(criticMode)) { setError('Invalid critic mode'); return }
    setSaving(true)
    setError('')
    try {
      const payload = {
        name: name.trim(),
        objective: description,
        working_dir: workingDir.trim(),
        kind: 'monitor' as const,
        ...schedulePayload(schedule),
        critic_mode: criticMode || 'none',
        tags,
      }
      if (initial) {
        await api.projects.update(initial.id, payload)
      } else {
        await api.projects.create(payload)
      }
      onSave()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  const generateDescription = async () => {
    if (!name.trim()) { setAiError('Enter a monitor name first'); return }
    setAiGenerating(true)
    setAiError('')
    try {
      const result = await api.projects.generateDescription(name, aiHint, aiProviderID)
      setDescription(result.description)
      setShowAI(false)
      setAiHint('')
    } catch (e: unknown) {
      setAiError(e instanceof Error ? e.message : 'Generation failed')
    } finally {
      setAiGenerating(false)
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
        <div className="flex items-center justify-between mb-1">
          <Label htmlFor="mon-desc">Objective — what should this monitor check and do?</Label>
          {providers.length > 0 && (
            <button
              type="button"
              onClick={() => { setShowAI(v => !v); setAiError('') }}
              className="text-xs text-violet-400 hover:text-violet-300 transition-colors flex items-center gap-1"
            >
              ✦ {showAI ? 'Hide AI assist' : 'Generate with AI'}
            </button>
          )}
        </div>
        {showAI && (
          <div className="mb-3 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-3">
            <p className="text-xs text-slate-400">Describe what you want this monitor to do and AI will write the description.</p>
            {providers.length > 1 && (
              <div>
                <Label htmlFor="ai-provider-mon">Generate using</Label>
                <ProviderSelect
                  id="ai-provider-mon"
                  value={aiProviderID}
                  onChange={setAiProviderID}
                  providers={providers}
                />
              </div>
            )}
            <div>
              <Label htmlFor="ai-hint-mon">Additional context <span className="text-slate-500 font-normal">(optional)</span></Label>
              <Textarea
                id="ai-hint-mon"
                value={aiHint}
                onChange={e => setAiHint(e.target.value)}
                rows={2}
                placeholder="e.g. Watch for tickets with priority=critical, notify if queue grows beyond 10"
              />
            </div>
            {aiError && <p className="text-xs text-red-400">{aiError}</p>}
            <div className="flex justify-end">
              <Button onClick={generateDescription} disabled={aiGenerating}>
                {aiGenerating ? 'Generating…' : '✦ Generate'}
              </Button>
            </div>
          </div>
        )}
        <Textarea id="mon-desc" value={description} onChange={e => setDescription(e.target.value)} rows={4}
          placeholder="What is this monitor's goal? e.g. Watch for critical Jira tickets and alert if queue exceeds 10" />
      </div>
      <div>
        <ScheduleEditor value={schedule} onChange={setSchedule} idPrefix="mon" />
      </div>
      <div>
        <Label>Tags <span className="text-slate-500 font-normal">(optional)</span></Label>
        <TagInput value={tags} onChange={setTags} suggestions={allTags} />
      </div>
      <div>
        <Label htmlFor="mon-wdir">
          Working Directory <span className="text-slate-500 font-normal">(optional)</span>
        </Label>
        <WorkingDirInput
          id="mon-wdir"
          value={workingDir}
          onChange={setWorkingDir}
          placeholder="/path/to/project"
        />
      </div>
      <div>
        <Label>Devil's Advocate</Label>
        <Select value={criticMode} onChange={e => setCriticMode(e.target.value)}>
          <option value="none">None — no critic review</option>
          <option value="builtin">Built-in — ephemeral contrarian review</option>
        </Select>
        <p className="text-xs text-slate-500 mt-1">Default critic mode applied to each monitor run. Can be overridden per task.</p>
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

function MonitorCard({ monitor, summary, agents, allAgents, providers, orchestrationEnabled, onPause, onResume, onArchive, onDelete, onRefresh }: {
  monitor: Project
  summary?: ProjectSummary
  agents: Agent[]
  allAgents: Agent[]
  providers: Provider[]
  orchestrationEnabled: boolean
  onPause: () => void
  onResume: () => void
  onArchive: () => void
  onDelete: () => void
  onRefresh: () => void
}) {
  const navigate = useNavigate()
  const [showEdit, setShowEdit] = useState(false)

  const hasSchedule = monitor.schedule_kind === 'daily'
    ? (monitor.schedule_times?.length ?? 0) > 0
    : !!monitor.schedule_interval
  const scheduleText = scheduleSummary(monitor)

  // Status dots from task health (same logic as ProjectListItem)
  const statusMap = summary?.tasks_by_status ?? {}
  const dots: { color: string; title: string }[] = []
  const hasRunning = (statusMap['running'] ?? 0) + (statusMap['queued'] ?? 0) + (statusMap['pending'] ?? 0) > 0
  const needsYou = (statusMap['awaiting_approval'] ?? 0) > 0
  const hasFailed = (statusMap['failed'] ?? 0) > 0
  const hasCompleted = (statusMap['completed'] ?? 0) > 0
  if (hasRunning) dots.push({ color: 'bg-violet-400', title: 'Running' })
  if (needsYou) dots.push({ color: 'bg-amber-400', title: 'Needs You' })
  if (hasFailed) dots.push({ color: 'bg-red-400', title: 'Failed' })
  if (hasCompleted && !hasRunning && !needsYou && !hasFailed) dots.push({ color: 'bg-emerald-400', title: 'All clear' })

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
      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden hover:border-slate-700 transition-colors">
        {/* Header row */}
        <div className="flex items-start gap-4 px-4 pt-4 pb-3">
          {/* Clickable title area */}
          <div
            className="flex-1 min-w-0 cursor-pointer"
            onClick={() => navigate(`/monitors/${monitor.id}`)}
          >
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-slate-400">⟳</span>
              <h3 className="font-medium text-white">{monitor.name}</h3>
              <Badge variant={monitor.status === 'active' ? 'success' : monitor.status === 'paused' ? 'warning' : 'muted'}>
                {monitor.status}
              </Badge>
              {monitor.tags?.map(t => <TagPill key={t} tag={t} />)}
              {dots.length > 0 && (
                <div className="flex items-center gap-1 ml-1">
                  {dots.map((d, i) => (
                    <span key={i} className={cn('w-1.5 h-1.5 rounded-full', d.color)} title={d.title} />
                  ))}
                </div>
              )}
            </div>
            {monitor.objective && (
              <p className="text-sm text-slate-400 line-clamp-1 mt-1 ml-5">
                {monitor.objective}
              </p>
            )}
            <div className="flex items-center gap-3 ml-5 mt-1.5 text-xs text-slate-500 flex-wrap">
              {hasSchedule
                ? <span className="text-violet-400 font-medium">⟳ {scheduleText}</span>
                : <span className="text-amber-500/80">Manual only</span>
              }
              {monitor.working_dir && (
                <span className="font-mono truncate" title={monitor.working_dir}>
                  📁 {monitor.working_dir.split('/').pop()}
                </span>
              )}
              <span>Created {timeAgo(monitor.created_at)}</span>
              {summary && summary.total_tasks > 0 && (
                <span>{summary.total_tasks} run{summary.total_tasks !== 1 ? 's' : ''}</span>
              )}
              <span
                className="text-slate-600 hover:text-slate-400 transition-colors cursor-pointer"
                onClick={e => { e.stopPropagation(); navigate(`/monitors/${monitor.id}`) }}
              >
                View runs →
              </span>
            </div>
          </div>

          {/* Action buttons */}
          <div className="flex gap-2 shrink-0">
            <Button variant="secondary" size="sm" onClick={() => setShowEdit(true)}>Edit</Button>
            {monitor.status === 'paused'
              ? <Button variant="secondary" size="sm" onClick={onResume}>Resume</Button>
              : <Button variant="secondary" size="sm" onClick={onPause}>Pause</Button>
            }
            <Button variant="secondary" size="sm" onClick={onArchive}>Archive</Button>
            <Button variant="danger" size="sm" onClick={onDelete}>Delete</Button>
          </div>
        </div>

        {/* Agents section */}
        <div className="border-t border-slate-800 px-4 py-3">
          <p className="text-xs text-slate-500 uppercase tracking-wide mb-2">Agents</p>
          <AgentsSection
            assigned={agents}
            allAgents={allAgents}
            showHeartbeat
            orchestrationEnabled={orchestrationEnabled}
            onAdd={addAgent}
            onRemove={removeAgent}
          />
        </div>
      </div>

      {showEdit && (
        <Modal title={`Edit: ${monitor.name}`} onClose={() => setShowEdit(false)} className="max-w-2xl">
          <MonitorForm
            initial={monitor}
            providers={providers}
            allTags={[]}
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
  const [summaries, setSummaries] = useState<Record<string, ProjectSummary>>({})
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [orchestrationEnabled, setOrchestrationEnabled] = useState(false)
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [fs, setFs] = useState<FilterSortState>({ search: '', activeTags: [], sort: 'created-desc' })

  const load = useCallback(async () => {
    try {
      const [mons, agents, provs, sums, settings] = await Promise.all([
        api.projects.list('monitor'),
        api.agents.list(),
        api.providers.list(),
        api.projects.summaries().catch(() => ({})),
        api.admin.getSettings().catch(() => null),
      ])
      setMonitors(mons)
      setAllAgents(agents)
      setProviders(provs)
      setSummaries(sums)
      setOrchestrationEnabled(settings?.dynamic_orchestration_enabled ?? false)
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

  const pauseMonitor = async (id: string) => {
    try {
      await api.projects.pause(id)
      load()
    } catch (error: unknown) { alert(getErrorMessage(error)) }
  }

  const resumeMonitor = async (id: string) => {
    try {
      await api.projects.restore(id)
      load()
    } catch (error: unknown) { alert(getErrorMessage(error)) }
  }

  const archiveMonitor = async (id: string, name: string) => {
    if (!confirm(`Archive "${name}"? It will disappear from this list but all run history is preserved. You can restore it from Settings → Archived.`)) return
    try {
      await api.projects.archive(id)
      load()
    } catch (error: unknown) { alert(getErrorMessage(error)) }
  }

  const deleteMonitor = async (id: string, name: string) => {
    if (!confirm(`Permanently delete "${name}" and all its run history? This cannot be undone.`)) return
    try {
      await api.projects.delete(id)
      load()
    } catch (error: unknown) { alert(getErrorMessage(error)) }
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
      ) : (() => {
        const allTags = collectAllTags(monitors)
        const displayed = applyFilterSort(monitors, fs)
        const groupByTag = fs.sort === 'tag'
        const groups: { label: string; items: Project[] }[] = []
        if (groupByTag) {
          const seen = new Set<string>()
          displayed.forEach(m => {
            const key = [...(m.tags ?? [])].sort()[0] ?? '(untagged)'
            if (!seen.has(key)) { seen.add(key); groups.push({ label: key, items: [] }) }
            groups.find(g => g.label === key)!.items.push(m)
          })
        }
        return (
          <div className="space-y-4">
            <FilterSortBar state={fs} onChange={setFs} allTags={allTags} total={monitors.length} filtered={displayed.length} />
            {displayed.length === 0 ? (
              <p className="text-slate-500 text-sm py-4">No monitors match your filter.</p>
            ) : groupByTag ? (
              <div className="space-y-6">
                {groups.map(g => (
                  <div key={g.label}>
                    <p className="text-xs font-semibold uppercase tracking-widest text-slate-500 mb-3">
                      {g.label === '(untagged)' ? 'Untagged' : g.label}
                      <span className="ml-2 font-normal normal-case tracking-normal text-slate-600">{g.items.length}</span>
                    </p>
                    <div className="grid gap-4">
                      {g.items.map(m => (
                        <MonitorCard key={m.id} monitor={m}
                          summary={summaries[m.id]}
                          agents={agentsByMonitor[m.id] ?? []} allAgents={allAgents} providers={providers}
                          orchestrationEnabled={orchestrationEnabled}
                          onPause={() => pauseMonitor(m.id)}
                          onResume={() => resumeMonitor(m.id)}
                          onArchive={() => archiveMonitor(m.id, m.name)}
                          onDelete={() => deleteMonitor(m.id, m.name)}
                          onRefresh={load} />
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="grid gap-4">
                {displayed.map(m => (
                  <MonitorCard key={m.id} monitor={m}
                    summary={summaries[m.id]}
                    agents={agentsByMonitor[m.id] ?? []} allAgents={allAgents} providers={providers}
                    orchestrationEnabled={orchestrationEnabled}
                    onPause={() => pauseMonitor(m.id)}
                    onResume={() => resumeMonitor(m.id)}
                    onArchive={() => archiveMonitor(m.id, m.name)}
                    onDelete={() => deleteMonitor(m.id, m.name)}
                    onRefresh={load} />
                ))}
              </div>
            )}
          </div>
        )
      })()}

      {showForm && (
        <Modal title="New Monitor" onClose={() => setShowForm(false)} className="max-w-2xl">
          <MonitorForm
            providers={providers}
            allTags={collectAllTags(monitors)}
            onSave={() => { setShowForm(false); load() }}
            onClose={() => setShowForm(false)}
          />
        </Modal>
      )}
    </div>
  )
}
