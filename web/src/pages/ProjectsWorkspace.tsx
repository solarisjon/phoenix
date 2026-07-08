/**
 * ProjectsWorkspace — three-pane email-client layout for Projects.
 *
 * LEFT:   project list with status dots
 * MIDDLE: task list (compact rows) + Files tab for selected project
 * RIGHT:  task detail / compose form / new-project form
 */

import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, type Project, type Task, type Agent, type ProjectSummary, type ProjectFileEntry, type Provider, type TaskTemplate } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { FollowUpThread } from '@/components/ui/follow-up-thread'
import { TaskDiffModal } from '@/components/task-diff-modal'
import { EditRetryModal } from '@/components/edit-retry-modal'
import { taskStatusVariant, taskStatusLabel, parseOutput, timeAgo, formatCost, getModelInfo } from '@/lib/utils'
import { cn } from '@/lib/utils'
import { phoenixWS } from '@/lib/ws'
import { WorkingDirInput } from '@/components/ui/working-dir-input'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type RightPane =
  | { type: 'empty' }
  | { type: 'task'; task: Task }
  | { type: 'compose' }
  | { type: 'new-project' }
  | { type: 'edit-project' }
  | { type: 'file'; entry: ProjectFileEntry; content: string; truncated: boolean }

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

export function ProjectsWorkspace() {
  const { id: projectId } = useParams<{ id?: string }>()
  const navigate = useNavigate()

  const [projects, setProjects] = useState<Project[]>([])
  const [summaries, setSummaries] = useState<Record<string, ProjectSummary>>({})
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [projectAgents, setProjectAgents] = useState<Agent[]>([])
  const [rightPane, setRightPane] = useState<RightPane>({ type: 'empty' })
  const [loading, setLoading] = useState(true)

  // Load project list + summaries + all agents once
  const loadBase = useCallback(async () => {
    const [projs, sums, agents] = await Promise.all([
      api.projects.list('project'),
      api.projects.summaries().catch(() => ({})),
      api.agents.list(),
    ])
    setProjects(projs)
    setSummaries(sums ?? {})
    setAllAgents(agents)
    setLoading(false)
  }, [])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadBase()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [loadBase])

  // Load tasks + project agents whenever selected project changes
  const loadProject = useCallback(async (id: string) => {
    const [ts, pas] = await Promise.all([
      api.tasks.list(id),
      api.projects.listAgents(id),
    ])
    setTasks(ts)
    setProjectAgents(pas)
  }, [])

  useEffect(() => {
    if (!projectId) return
    const timer = window.setTimeout(() => {
      void loadProject(projectId)
    }, 0)
    return () => window.clearTimeout(timer)
  }, [projectId, loadProject])

  // Auto-select first project when none selected and list is ready
  useEffect(() => {
    if (!loading && !projectId && projects.length > 0) {
      navigate(`/projects/${projects[0].id}`, { replace: true })
    }
  }, [loading, projectId, projects, navigate])

  const selectedProject = projects.find(p => p.id === projectId) ?? null
  const displayedTasks = projectId ? tasks : []
  const displayedProjectAgents = projectId ? projectAgents : []
  const displayedRightPane = projectId && (
    (rightPane.type === 'task' && rightPane.task.project_id !== projectId) ||
    (rightPane.type === 'file' && !selectedProject)
  ) ? { type: 'empty' } satisfies RightPane : rightPane

  const refreshTasks = useCallback(async () => {
    if (!projectId) return
    const [ts, sums] = await Promise.all([
      api.tasks.list(projectId),
      api.projects.summaries().catch(() => ({})),
    ])
    setTasks(ts)
    setSummaries(sums ?? {})
  }, [projectId])

  return (
    <div className="flex h-full overflow-hidden">
      {/* LEFT PANE — project list */}
      <LeftPane
        projects={projects}
        summaries={summaries}
        selectedId={projectId}
        onSelect={id => navigate(`/projects/${id}`)}
        onNewProject={() => setRightPane({ type: 'new-project' })}
      />

      {/* MIDDLE PANE — task list */}
      <MiddlePane
        project={selectedProject}
        key={selectedProject?.id ?? 'none'}
        projectAgents={displayedProjectAgents}
        allAgents={allAgents}
        tasks={displayedTasks}
        selectedTask={displayedRightPane.type === 'task' ? displayedRightPane.task : null}
        selectedFile={displayedRightPane.type === 'file' ? displayedRightPane.entry : null}
        onTaskClick={task => setRightPane({ type: 'task', task })}
        onFileClick={async (entry) => {
          const { content, truncated } = await api.projects.getFileContent(
            projectId!, entry.rel_path
          ).catch(() => ({ content: '(could not load file)', truncated: false }))
          setRightPane({ type: 'file', entry, content, truncated })
        }}
        onCompose={() => setRightPane({ type: 'compose' })}
        onEdit={() => setRightPane({ type: 'edit-project' })}
        onAgentAssigned={() => projectId && loadProject(projectId)}
        onTasksRefresh={refreshTasks}
        onProjectObjectiveUpdated={loadBase}
      />

      {/* RIGHT PANE — detail / compose / new-project */}
      <RightPaneArea
        pane={displayedRightPane}
        project={selectedProject}
        projectAgents={displayedProjectAgents}
        allAgents={allAgents}
        tasks={tasks}
        onClose={() => setRightPane({ type: 'empty' })}
        onTaskCreated={async () => {
          await refreshTasks()
          setRightPane({ type: 'empty' })
        }}
        onProjectCreated={async (id) => {
          await loadBase()
          navigate(`/projects/${id}`)
          setRightPane({ type: 'empty' })
        }}
        onProjectUpdated={async () => {
          await loadBase()
          if (projectId) await loadProject(projectId)
          setRightPane({ type: 'empty' })
        }}
        onProjectRemoved={async () => {
          await loadBase()
          navigate('/projects')
          setRightPane({ type: 'empty' })
        }}
        onTaskUpdated={refreshTasks}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// LEFT PANE
// ---------------------------------------------------------------------------

interface LeftPaneProps {
  projects: Project[]
  summaries: Record<string, ProjectSummary>
  selectedId: string | undefined
  onSelect: (id: string) => void
  onNewProject: () => void
}

function LeftPane({ projects, summaries, selectedId, onSelect, onNewProject }: LeftPaneProps) {
  const [search, setSearch] = useState('')
  const filtered = projects.filter(p =>
    p.name.toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div className="flex flex-col w-64 shrink-0 border-r border-slate-800 bg-slate-950">
      <div className="flex items-center justify-between px-3 py-3 border-b border-slate-800">
        <span className="text-sm font-semibold text-slate-200">Projects</span>
        <button
          onClick={onNewProject}
          className="text-xs text-violet-400 hover:text-violet-300 font-medium"
          title="New project"
        >
          + New
        </button>
      </div>

      <div className="px-2 py-2">
        <input
          type="text"
          placeholder="Search…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="w-full rounded bg-slate-800 border border-slate-700 text-xs text-slate-300 px-2 py-1.5 focus:outline-none focus:border-violet-500"
        />
      </div>

      <div className="flex-1 overflow-y-auto">
        {filtered.map(p => (
          <ProjectListItem
            key={p.id}
            project={p}
            summary={summaries[p.id]}
            selected={p.id === selectedId}
            onClick={() => onSelect(p.id)}
          />
        ))}
        {filtered.length === 0 && (
          <p className="text-xs text-slate-500 px-3 py-4">No projects found.</p>
        )}
      </div>
    </div>
  )
}

interface ProjectListItemProps {
  project: Project
  summary?: ProjectSummary
  selected: boolean
  onClick: () => void
}

function ProjectListItem({ project, summary, selected, onClick }: ProjectListItemProps) {
  const statusMap = summary?.tasks_by_status ?? {}

  // Status dot logic: show highest-priority dot only if tasks exist
  const dots: { color: string; title: string }[] = []
  const hasRunning = (statusMap['running'] ?? 0) + (statusMap['queued'] ?? 0) + (statusMap['pending'] ?? 0) > 0
  const needsYou = (statusMap['awaiting_approval'] ?? 0) > 0
  const hasFailed = (statusMap['failed'] ?? 0) > 0
  const hasCompleted = (statusMap['completed'] ?? 0) > 0

  if (hasRunning) dots.push({ color: 'bg-violet-400', title: 'Running' })
  if (needsYou) dots.push({ color: 'bg-amber-400', title: 'Needs You' })
  if (hasFailed) dots.push({ color: 'bg-red-400', title: 'Failed' })
  if (hasCompleted && !hasRunning && !needsYou && !hasFailed) {
    dots.push({ color: 'bg-emerald-400', title: 'Completed' })
  }

  return (
    <button
      onClick={onClick}
      className={cn(
        'w-full text-left px-3 py-2.5 hover:bg-slate-800 transition-colors border-b border-slate-800/60',
        selected && 'bg-slate-800 border-l-2 border-l-violet-500'
      )}
    >
      <div className="flex items-start justify-between gap-1">
        <span className={cn('text-sm truncate', selected ? 'text-white font-medium' : 'text-slate-300')}>
          {project.name}
        </span>
        {dots.length > 0 && (
          <div className="flex items-center gap-1 shrink-0 pt-0.5">
            {dots.map((d, i) => (
              <span key={i} className={cn('w-1.5 h-1.5 rounded-full', d.color)} title={d.title} />
            ))}
          </div>
        )}
      </div>
      <div className="flex items-center gap-2 mt-0.5">
        <span className="text-xs text-slate-500">
          {summary ? `${summary.total_tasks} task${summary.total_tasks !== 1 ? 's' : ''}` : 'No tasks'}
        </span>
        {summary?.last_activity && (
          <span className="text-xs text-slate-600">{timeAgo(summary.last_activity)}</span>
        )}
      </div>
    </button>
  )
}

// ---------------------------------------------------------------------------
// MIDDLE PANE
// ---------------------------------------------------------------------------

interface MiddlePaneProps {
  project: Project | null
  projectAgents: Agent[]
  allAgents: Agent[]
  tasks: Task[]
  selectedTask: Task | null
  selectedFile: ProjectFileEntry | null
  onTaskClick: (task: Task) => void
  onFileClick: (entry: ProjectFileEntry) => void
  onCompose: () => void
  onEdit: () => void
  onAgentAssigned: () => void
  onTasksRefresh: () => void
  onProjectObjectiveUpdated: () => void
}

// Status-section config — order determines priority shown top-to-bottom.
const TASK_SECTIONS: { label: string; statuses: Task['status'][]; color: string; sectionKey: string }[] = [
  { label: 'Needs Attention', statuses: ['awaiting_approval'], color: 'text-amber-400', sectionKey: 'attention' },
  { label: 'Running',         statuses: ['running', 'queued'],  color: 'text-violet-400', sectionKey: 'running' },
  { label: 'Failed',          statuses: ['failed'],             color: 'text-red-400',    sectionKey: 'failed' },
  { label: 'Completed',       statuses: ['completed'],          color: 'text-emerald-400', sectionKey: 'completed' },
]

function MiddlePane({
  project, projectAgents, allAgents, tasks, selectedTask, selectedFile,
  onTaskClick, onFileClick, onCompose, onEdit, onAgentAssigned, onTasksRefresh, onProjectObjectiveUpdated,
}: MiddlePaneProps) {
  const [tab, setTab] = useState<'tasks' | 'files'>('tasks')
  const [assignOpen, setAssignOpen] = useState(false)
  const [assignAgentId, setAssignAgentId] = useState('')
  const [assigning, setAssigning] = useState(false)
  const [files, setFiles] = useState<ProjectFileEntry[]>([])
  const [filesLoading, setFilesLoading] = useState(false)

  // Objective inline-edit
  const [editingObjective, setEditingObjective] = useState(false)
  const [objectiveDraft, setObjectiveDraft] = useState('')
  const [savingObjective, setSavingObjective] = useState(false)
  const objectiveRef = useRef<HTMLTextAreaElement>(null)
  const [objectiveAIOpen, setObjectiveAIOpen] = useState(false)
  const [objectiveAIHint, setObjectiveAIHint] = useState('')
  const [objectiveAIProviders, setObjectiveAIProviders] = useState<Provider[]>([])
  const [objectiveAIProviderID, setObjectiveAIProviderID] = useState('')
  const [objectiveAIGenerating, setObjectiveAIGenerating] = useState(false)

  // Collapsed sections — completed collapsed by default; others expanded
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => {
    if (!project) return {}
    const stored = localStorage.getItem(`phoenix-sections-${project?.id}`)
    return stored ? JSON.parse(stored) : { completed: true }
  })

  // Diff selection — pick exactly 2 completed tasks to compare
  const [diffSelection, setDiffSelection] = useState<string[]>([])
  const [diffModalOpen, setDiffModalOpen] = useState(false)

  const toggleDiffSelection = (id: string) => {
    setDiffSelection(prev =>
      prev.includes(id) ? prev.filter(x => x !== id) : prev.length < 2 ? [...prev, id] : [prev[1], id]
    )
  }

  // History (completed incl. dismissed) — loaded lazily when completed section expanded
  const [history, setHistory] = useState<Task[]>([])
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyLoaded, setHistoryLoaded] = useState(false)

  // AI suggestion card
  const [suggestion, setSuggestion] = useState<{ title: string; description: string } | null>(null)
  const [suggesting, setSuggesting] = useState(false)
  const [runningFromSuggest, setRunningFromSuggest] = useState(false)

  const unassignedAgents = allAgents.filter(a => !projectAgents.find(pa => pa.id === a.id))

  // Load file list when Files tab is selected
  useEffect(() => {
    if (tab !== 'files' || !project) return
    const timer = window.setTimeout(() => {
      setFilesLoading(true)
      void api.projects.listFiles(project.id)
        .then(setFiles)
        .catch(() => setFiles([]))
        .finally(() => setFilesLoading(false))
    }, 0)
    return () => window.clearTimeout(timer)
  }, [project, tab])

  // WS: refresh task list when a task status changes for the current project
  useEffect(() => {
    if (!project) return
    const unsub = phoenixWS.on(ev => {
      if (ev.type === 'task.status_changed') {
        const p = ev.payload
        if (p.project_id === project.id) {
          onTasksRefresh()
          // If completed section is open, refresh history too
          if (!collapsed['completed'] && historyLoaded) {
            setHistoryLoaded(false)
          }
        }
      }
    })
    return unsub
  }, [collapsed, historyLoaded, onTasksRefresh, project])

  const toggleSection = (key: string) => {
    setCollapsed(prev => {
      const next = { ...prev, [key]: !prev[key] }
      if (project) localStorage.setItem(`phoenix-sections-${project.id}`, JSON.stringify(next))
      return next
    })
  }

  // Load history when completed section is expanded for the first time
  useEffect(() => {
    if (!project || collapsed['completed'] || historyLoaded) return
    const timer = window.setTimeout(() => {
      setHistoryLoading(true)
      void api.projects.history(project.id)
        .then(h => { setHistory(h); setHistoryLoaded(true) })
        .catch(() => {})
        .finally(() => setHistoryLoading(false))
    }, 0)
    return () => window.clearTimeout(timer)
  }, [collapsed, historyLoaded, project])

  const handleAssign = async () => {
    if (!project || !assignAgentId) return
    setAssigning(true)
    try {
      await api.projects.assignAgent(project.id, assignAgentId)
      setAssignOpen(false)
      setAssignAgentId('')
      onAgentAssigned()
    } finally {
      setAssigning(false)
    }
  }

  const startEditObjective = () => {
    setObjectiveDraft(project?.objective ?? '')
    setEditingObjective(true)
    setObjectiveAIOpen(false)
    if (objectiveAIProviders.length === 0) {
      api.providers.list().then(list => {
        setObjectiveAIProviders(list)
        setObjectiveAIProviderID(list.find(p => p.type === 'llm')?.id ?? list[0]?.id ?? '')
      }).catch(() => {})
    }
    setTimeout(() => objectiveRef.current?.focus(), 0)
  }

  const generateObjective = async () => {
    if (!project) return
    setObjectiveAIGenerating(true)
    try {
      const result = await api.projects.generateDescription(project.name, objectiveAIHint, objectiveAIProviderID)
      setObjectiveDraft(result.description)
      setObjectiveAIOpen(false)
      setObjectiveAIHint('')
    } catch { /* ignore */ } finally {
      setObjectiveAIGenerating(false)
    }
  }

  const saveObjective = async () => {
    if (!project) return
    setSavingObjective(true)
    try {
      await api.projects.update(project.id, { ...project, objective: objectiveDraft })
      onProjectObjectiveUpdated()
    } finally {
      setSavingObjective(false)
      setEditingObjective(false)
    }
  }

  const handleSuggest = async () => {
    if (!project) return
    setSuggesting(true)
    setSuggestion(null)
    try {
      const res = await api.projects.suggest(project.id)
      if (res.suggestions?.length > 0) setSuggestion(res.suggestions[0])
    } catch { /* ignore */ }
    finally { setSuggesting(false) }
  }

  const runSuggestion = async () => {
    if (!project || !suggestion || projectAgents.length === 0) return
    setRunningFromSuggest(true)
    try {
      await api.tasks.create({
        project_id: project.id,
        agent_id: projectAgents[0].id,
        title: suggestion.title,
        description: suggestion.description,
      })
      setSuggestion(null)
      onTasksRefresh()
    } catch { /* ignore */ }
    finally { setRunningFromSuggest(false) }
  }

  if (!project) {
    return (
      <div className="flex-1 flex items-center justify-center text-slate-600 text-sm">
        Select a project
      </div>
    )
  }

  // Build counts from active tasks (non-dismissed only)
  const countByStatus = (statuses: Task['status'][]) =>
    tasks.filter(t => statuses.includes(t.status)).length

  const totalDone = tasks.filter(t => t.status === 'completed').length
  const totalFailed = tasks.filter(t => t.status === 'failed').length
  const totalRunning = tasks.filter(t => t.status === 'running' || t.status === 'queued').length
  const totalAttention = tasks.filter(t => t.status === 'awaiting_approval').length

  // Merge active completed tasks with history (de-dup by id)
  const completedActive = tasks.filter(t => t.status === 'completed')
  const completedAll = historyLoaded
    ? [...completedActive, ...history.filter(h => !completedActive.find(t => t.id === h.id))]
    : completedActive

  return (
    <div className="flex flex-col w-80 shrink-0 border-r border-slate-800 bg-slate-900">
      {/* ── Project header ── */}
      <div className="px-4 pt-3 pb-2 border-b border-slate-800">
        <div className="flex items-start justify-between gap-2 mb-1.5">
          <h2 className="text-sm font-semibold text-white truncate">{project.name}</h2>
          <button
            onClick={onEdit}
            className="shrink-0 text-xs text-slate-500 hover:text-slate-300 transition-colors"
            title="Edit project"
          >
            ✎
          </button>
        </div>

        {/* Inline-editable objective */}
        {editingObjective ? (
          <div className="mb-2">
            {objectiveAIOpen && (
              <div className="mb-2 rounded border border-violet-800/50 bg-violet-950/30 p-2 space-y-1.5">
                {objectiveAIProviders.length > 1 && (
                  <select
                    value={objectiveAIProviderID}
                    onChange={e => setObjectiveAIProviderID(e.target.value)}
                    className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1 focus:outline-none focus:border-violet-500"
                  >
                    {objectiveAIProviders.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                  </select>
                )}
                <textarea
                  value={objectiveAIHint}
                  onChange={e => setObjectiveAIHint(e.target.value)}
                  rows={2}
                  placeholder="Additional context (optional)…"
                  className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1 resize-none focus:outline-none focus:border-violet-500"
                />
                <div className="flex justify-end">
                  <button
                    onClick={generateObjective}
                    disabled={objectiveAIGenerating}
                    className="text-[10px] bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-2 py-1"
                  >
                    {objectiveAIGenerating ? 'Generating…' : '✦ Generate'}
                  </button>
                </div>
              </div>
            )}
            <textarea
              ref={objectiveRef}
              value={objectiveDraft}
              onChange={e => setObjectiveDraft(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); saveObjective() } if (e.key === 'Escape') setEditingObjective(false) }}
              rows={3}
              className="w-full text-xs bg-slate-800 border border-violet-500/50 text-slate-200 rounded px-2 py-1.5 resize-none focus:outline-none"
              placeholder="What is this project trying to accomplish?"
            />
            <div className="flex items-center gap-2 mt-1">
              <button onClick={saveObjective} disabled={savingObjective}
                className="text-[10px] px-2 py-0.5 rounded bg-violet-600 text-white hover:bg-violet-500 disabled:opacity-50">
                {savingObjective ? 'Saving…' : 'Save'}
              </button>
              <button onClick={() => setEditingObjective(false)}
                className="text-[10px] px-2 py-0.5 rounded text-slate-500 hover:text-slate-300">
                Cancel
              </button>
              <div className="flex-1" />
              {objectiveAIProviders.length > 0 && (
                <button
                  onClick={() => setObjectiveAIOpen(v => !v)}
                  className="text-[10px] text-violet-400 hover:text-violet-300 transition-colors"
                >
                  ✦ {objectiveAIOpen ? 'Hide AI' : 'Generate with AI'}
                </button>
              )}
            </div>
          </div>
        ) : (
          <button
            onClick={startEditObjective}
            className="w-full text-left mb-2 group"
            title="Click to set objective"
          >
            <div className="text-[10px] text-slate-600 uppercase tracking-wide mb-0.5">
              Objective <span className="text-slate-700 group-hover:text-slate-500 transition-colors">· edit</span>
            </div>
            <div className={cn('text-xs leading-relaxed', project.objective ? 'text-slate-400' : 'text-slate-600 italic')}>
              {project.objective || 'Click to add an objective…'}
            </div>
          </button>
        )}

        {/* Status counts + actions */}
        <div className="flex items-center gap-1.5 flex-wrap">
          {totalDone > 0 && (
            <button onClick={() => !collapsed['completed'] || toggleSection('completed')}
              className="text-[10px] px-1.5 py-0.5 rounded bg-emerald-900/40 text-emerald-400">
              ✓ {totalDone}
            </button>
          )}
          {totalFailed > 0 && (
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-red-900/40 text-red-400">✗ {totalFailed}</span>
          )}
          {totalRunning > 0 && (
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-violet-900/40 text-violet-400">● {totalRunning}</span>
          )}
          {totalAttention > 0 && (
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-900/40 text-amber-400">⚠ {totalAttention}</span>
          )}
          <div className="flex-1" />
          <button
            onClick={handleSuggest}
            disabled={suggesting}
            className="text-[10px] px-2 py-0.5 rounded bg-blue-900/40 text-blue-300 hover:bg-blue-900/60 disabled:opacity-50 transition-colors"
            title="Ask AI to suggest the next action"
          >
            {suggesting ? '…' : '✦ Suggest'}
          </button>
        </div>

        {/* Agent roster pills */}
        <div className="flex flex-wrap items-center gap-1 mt-2">
          {projectAgents.map(a => (
            <span key={a.id} className="text-[10px] bg-slate-800 text-slate-400 rounded-full px-2 py-0.5">
              {a.name}
            </span>
          ))}
          <button onClick={() => setAssignOpen(v => !v)} className="text-[10px] text-violet-400 hover:text-violet-300">
            + Assign
          </button>
        </div>

        {/* Assign agent popover */}
        {assignOpen && (
          <div className="mt-2 p-2 bg-slate-800 rounded border border-slate-700 flex gap-2">
            <select value={assignAgentId} onChange={e => setAssignAgentId(e.target.value)}
              className="flex-1 text-xs bg-slate-900 border border-slate-700 text-slate-300 rounded px-2 py-1">
              <option value="">Select agent…</option>
              {unassignedAgents.map(a => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
            <button onClick={handleAssign} disabled={!assignAgentId || assigning}
              className="text-xs bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-2 py-1">
              {assigning ? '…' : 'Add'}
            </button>
          </div>
        )}
      </div>

      {/* Tabs */}
      <div className="flex items-center border-b border-slate-800 px-4">
        {(['tasks', 'files'] as const).map(t => (
          <button key={t} onClick={() => setTab(t)}
            className={cn(
              'text-xs py-2 mr-4 border-b-2 transition-colors capitalize',
              tab === t ? 'border-violet-500 text-white' : 'border-transparent text-slate-500 hover:text-slate-300'
            )}>
            {t}
          </button>
        ))}
        <div className="flex-1" />
        {tab === 'tasks' && (
          <button onClick={onCompose} className="text-xs text-violet-400 hover:text-violet-300 py-2">
            + Task
          </button>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto">
        {tab === 'tasks' && (
          <div>
            {/* AI suggestion card */}
            {suggestion && (
              <div className="mx-3 mt-3 mb-1 bg-emerald-950/60 border border-emerald-800/50 rounded-lg p-3">
                <div className="text-[9px] text-emerald-400 uppercase tracking-wide font-semibold mb-1">✦ Suggested next action</div>
                <div className="text-xs text-slate-200 font-medium mb-1">{suggestion.title}</div>
                <div className="text-[10px] text-slate-400 mb-2 leading-relaxed">{suggestion.description}</div>
                <div className="flex gap-2">
                  <button onClick={runSuggestion} disabled={runningFromSuggest || projectAgents.length === 0}
                    className="text-[10px] px-2 py-0.5 rounded bg-emerald-700 text-emerald-100 hover:bg-emerald-600 disabled:opacity-50 transition-colors">
                    {runningFromSuggest ? 'Starting…' : '▶ Run this'}
                  </button>
                  <button onClick={() => setSuggestion(null)}
                    className="text-[10px] px-2 py-0.5 rounded text-slate-500 hover:text-slate-300">
                    ✕
                  </button>
                </div>
              </div>
            )}

            {/* Status-grouped sections */}
            {TASK_SECTIONS.map(section => {
              // For completed, use merged active+history list
              const sectionTasks = section.sectionKey === 'completed'
                ? completedAll
                : tasks.filter(t => section.statuses.includes(t.status))

              // Always show count from active tasks in header; history augments when expanded
              const activeCount = countByStatus(section.statuses)
              const displayCount = section.sectionKey === 'completed' && historyLoaded
                ? completedAll.length
                : activeCount

              if (activeCount === 0 && section.sectionKey !== 'completed') return null

              const isCollapsed = collapsed[section.sectionKey] ?? false

              return (
                <div key={section.sectionKey}>
                  {/* Section header */}
                  <button
                    onClick={() => toggleSection(section.sectionKey)}
                    className="w-full flex items-center justify-between px-4 py-1.5 hover:bg-slate-800/40 transition-colors"
                  >
                    <span className={cn('text-[10px] font-semibold uppercase tracking-wide', section.color)}>
                      {section.label}
                    </span>
                    <div className="flex items-center gap-1.5">
                      <span className="text-[10px] text-slate-600">{displayCount}</span>
                      <span className="text-[10px] text-slate-600">{isCollapsed ? '▶' : '▼'}</span>
                    </div>
                  </button>

                  {/* Tasks in section */}
                  {!isCollapsed && (
                    section.sectionKey === 'completed' && historyLoading ? (
                      <p className="text-[10px] text-slate-500 px-4 py-2">Loading history…</p>
                    ) : sectionTasks.length === 0 ? (
                      <p className="text-[10px] text-slate-600 px-4 py-1.5 italic">None</p>
                    ) : (
                      <>
                        {section.sectionKey === 'completed' && diffSelection.length === 2 && (
                          <div className="mx-3 my-1.5 flex items-center gap-2 rounded bg-violet-900/30 border border-violet-700/40 px-2 py-1.5">
                            <span className="text-[10px] text-violet-300 flex-1">2 runs selected — compare outputs?</span>
                            <button
                              onClick={() => setDiffModalOpen(true)}
                              className="text-[10px] px-2 py-0.5 rounded bg-violet-600 text-white hover:bg-violet-500"
                            >
                              Compare
                            </button>
                            <button
                              onClick={() => setDiffSelection([])}
                              className="text-[10px] text-slate-500 hover:text-slate-300 px-1"
                            >
                              ✕
                            </button>
                          </div>
                        )}
                        {sectionTasks.map(task => (
                          <TaskRow
                            key={task.id}
                            task={task}
                            agents={projectAgents}
                            selected={selectedTask?.id === task.id}
                            onClick={() => onTaskClick(task)}
                            onDiffToggle={section.sectionKey === 'completed' ? toggleDiffSelection : undefined}
                            isDiffSelected={diffSelection.includes(task.id)}
                          />
                        ))}
                      </>
                    )
                  )}
                </div>
              )
            })}

            {/* Empty state — no tasks at all */}
            {tasks.length === 0 && !suggestion && (
              <p className="text-xs text-slate-500 px-4 py-6">
                No tasks yet. Create one with <span className="text-violet-400">+ Task</span>, or use{' '}
                <button onClick={handleSuggest} className="text-blue-400 hover:text-blue-300">✦ Suggest</button>{' '}
                to get an AI recommendation.
              </p>
            )}
          </div>
        )}
        {tab === 'files' && (
          filesLoading
            ? <p className="text-xs text-slate-500 px-4 py-6">Loading…</p>
            : files.length === 0
              ? <p className="text-xs text-slate-500 px-4 py-6">No files in project working directory.</p>
              : files.map(f => (
                  <FileRow
                    key={f.rel_path}
                    entry={f}
                    selected={selectedFile?.rel_path === f.rel_path}
                    onClick={() => onFileClick(f)}
                  />
                ))
        )}
      </div>

      {/* Diff modal — shown when user hits Compare with 2 tasks selected */}
      {diffModalOpen && diffSelection.length === 2 && (() => {
        const allTasks = [...tasks, ...history]
        const t0 = allTasks.find(t => t.id === diffSelection[0])
        const t1 = allTasks.find(t => t.id === diffSelection[1])
        if (!t0 || !t1) return null
        const [older, newer] = t0.created_at <= t1.created_at ? [t0, t1] : [t1, t0]
        return (
          <TaskDiffModal
            older={older}
            newer={newer}
            onClose={() => { setDiffModalOpen(false); setDiffSelection([]) }}
          />
        )
      })()}
    </div>
  )
}

interface TaskRowProps {
  task: Task
  agents: Agent[]
  selected: boolean
  onClick: () => void
  onDiffToggle?: (id: string) => void
  isDiffSelected?: boolean
}

function TaskRow({ task, agents, selected, onClick, onDiffToggle, isDiffSelected }: TaskRowProps) {
  const agent = agents.find(a => a.id === task.agent_id)
  const variant = taskStatusVariant(task.status)

  const borderColor: Record<string, string> = {
    success: 'border-l-emerald-500',
    warning: 'border-l-amber-500',
    danger: 'border-l-red-500',
    info: 'border-l-violet-500',
    muted: 'border-l-slate-600',
    default: 'border-l-slate-600',
  }

  return (
    <div className={cn(
      'flex items-start border-b border-slate-800/60 border-l-2',
      borderColor[variant] ?? 'border-l-slate-600',
    )}>
      {onDiffToggle && (
        <button
          onClick={e => { e.stopPropagation(); onDiffToggle(task.id) }}
          className={cn(
            'shrink-0 self-stretch px-2 flex items-center justify-center hover:bg-slate-800/40 transition-colors',
            isDiffSelected ? 'text-violet-400' : 'text-slate-700 hover:text-slate-500'
          )}
          title={isDiffSelected ? 'Deselect for diff' : 'Select for diff'}
        >
          <span className="text-xs">{isDiffSelected ? '☑' : '☐'}</span>
        </button>
      )}
      <button
        onClick={onClick}
        className={cn(
          'flex-1 text-left px-3 py-2.5 hover:bg-slate-800/60 transition-colors min-w-0',
          selected && 'bg-slate-800/80'
        )}
      >
        <div className="flex items-start justify-between gap-2">
          <span className={cn(
            'text-xs leading-snug line-clamp-2',
            task.status === 'awaiting_approval' ? 'font-semibold text-white' : 'text-slate-300'
          )}>
            {task.title || 'Untitled task'}
          </span>
          <Badge variant={variant} className="shrink-0 text-[10px] py-0 px-1.5">
            {taskStatusLabel(task.status)}
          </Badge>
        </div>
        <div className="flex items-center gap-2 mt-1">
          {agent && <span className="text-[11px] text-slate-500">{agent.name}</span>}
          <span className="text-[11px] text-slate-600">{timeAgo(task.created_at)}</span>
        </div>
      </button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// FILE ROW
// ---------------------------------------------------------------------------

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

const EXT_COLORS: Record<string, string> = {
  md: 'bg-blue-900/60 text-blue-300',
  txt: 'bg-slate-700 text-slate-300',
  html: 'bg-orange-900/60 text-orange-300',
  json: 'bg-yellow-900/60 text-yellow-300',
  go: 'bg-cyan-900/60 text-cyan-300',
  py: 'bg-green-900/60 text-green-300',
  ts: 'bg-blue-900/60 text-blue-300',
  tsx: 'bg-blue-900/60 text-blue-300',
  js: 'bg-yellow-900/60 text-yellow-300',
}

interface FileRowProps {
  entry: ProjectFileEntry
  selected: boolean
  onClick: () => void
}

function FileRow({ entry, selected, onClick }: FileRowProps) {
  const extColor = EXT_COLORS[entry.ext] ?? 'bg-slate-700 text-slate-400'

  return (
    <button
      onClick={onClick}
      className={cn(
        'w-full text-left px-3 py-2 hover:bg-slate-800/60 transition-colors border-b border-slate-800/60 flex items-center gap-2',
        selected && 'bg-slate-800/80'
      )}
    >
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-sm text-slate-200 truncate">{entry.name}</span>
          {entry.is_artifact && (
            <span className="text-[9px] bg-violet-700/60 text-violet-300 rounded px-1 py-0.5 shrink-0">artifact</span>
          )}
        </div>
        <div className="flex items-center gap-2 mt-0.5">
          <span className={cn('text-[10px] rounded px-1 py-0.5 font-mono shrink-0', extColor)}>
            {entry.ext || 'file'}
          </span>
          <span className="text-[11px] text-slate-500 truncate">{entry.rel_path}</span>
        </div>
      </div>
      <div className="flex flex-col items-end shrink-0 text-[10px] text-slate-500">
        <span>{formatBytes(entry.size_bytes)}</span>
        <span>{timeAgo(entry.modified_at)}</span>
      </div>
    </button>
  )
}

// ---------------------------------------------------------------------------
// RIGHT PANE AREA
// ---------------------------------------------------------------------------

interface RightPaneAreaProps {
  pane: RightPane
  project: Project | null
  projectAgents: Agent[]
  allAgents: Agent[]
  tasks: Task[]
  onClose: () => void
  onTaskCreated: () => void
  onProjectCreated: (id: string) => void
  onProjectUpdated: () => void
  onProjectRemoved: () => void
  onTaskUpdated: () => void
}

function RightPaneArea({
  pane, project, projectAgents, allAgents, tasks, onClose, onTaskCreated, onProjectCreated, onProjectUpdated, onProjectRemoved, onTaskUpdated,
}: RightPaneAreaProps) {
  return (
    <div className="flex-1 flex flex-col overflow-hidden bg-slate-900">
      {pane.type === 'empty' && (
        <div className="flex-1 flex items-center justify-center text-slate-600 text-sm">
          {project ? 'Select a task or create one.' : 'Select a project.'}
        </div>
      )}

      {pane.type === 'task' && (
        <TaskDetailView
          task={pane.task}
          agents={projectAgents}
          onClose={onClose}
          onUpdated={onTaskUpdated}
        />
      )}

      {pane.type === 'compose' && project && (
        <TaskComposeForm
          project={project}
          projectAgents={projectAgents}
          tasks={tasks}
          onCreated={onTaskCreated}
          onCancel={onClose}
        />
      )}

      {pane.type === 'new-project' && (
        <NewProjectForm
          allAgents={allAgents}
          onCreated={onProjectCreated}
          onCancel={onClose}
        />
      )}

      {pane.type === 'edit-project' && project && (
        <EditProjectForm
          project={project}
          onSaved={onProjectUpdated}
          onRemoved={onProjectRemoved}
          onCancel={onClose}
        />
      )}

      {pane.type === 'file' && (
        <FilePreviewView
          entry={pane.entry}
          content={pane.content}
          truncated={pane.truncated}
          onClose={onClose}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// TASK DETAIL VIEW
// ---------------------------------------------------------------------------

interface TaskDetailViewProps {
  task: Task
  agents: Agent[]
  onClose: () => void
  onUpdated: () => void
}

function TaskDetailView({ task, agents, onClose, onUpdated }: TaskDetailViewProps) {
  const [approving, setApproving] = useState(false)
  const [approvalNote, setApprovalNote] = useState('')
  const [providers, setProviders] = useState<Provider[]>([])
  const [actioning, setActioning] = useState<string | null>(null)
  const [showEditRetry, setShowEditRetry] = useState(false)
  const output = parseOutput(task.output)
  const agent = agents.find(a => a.id === task.agent_id)
  const modelInfo = getModelInfo(agent, providers)

  useEffect(() => {
    api.providers.list().then(setProviders).catch(() => {})
  }, [])

  const handleApprove = async () => {
    setApproving(true)
    try {
      await api.tasks.followUp(task.id, approvalNote || 'Approved — please continue.')
      onUpdated()
    } finally {
      setApproving(false)
    }
  }

  const handleReject = async () => {
    setApproving(true)
    try {
      await api.tasks.followUp(task.id, approvalNote || 'Rejected — please revise.')
      onUpdated()
    } finally {
      setApproving(false)
    }
  }

  const handleAction = async (action: 'retry' | 'bump' | 'cancel' | 'force-reset' | 'dismiss') => {
    setActioning(action)
    try {
      if (action === 'retry') await api.tasks.retry(task.id)
      else if (action === 'bump') await api.tasks.bump(task.id)
      else if (action === 'cancel') await api.tasks.cancel(task.id)
      else if (action === 'force-reset') await api.tasks.forceReset(task.id)
      else if (action === 'dismiss') await api.tasks.dismiss(task.id)
      onUpdated()
    } catch { /* ignore */ } finally {
      setActioning(null)
    }
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="flex items-start gap-3 px-4 py-3 border-b border-slate-800 shrink-0">
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-white leading-snug">{task.title || 'Untitled task'}</h3>
          <div className="flex items-center gap-1.5 flex-wrap mt-1">
            {task.task_type === 'orchestration' && (
              <span className="text-[10px] font-medium text-violet-400 bg-violet-900/30 border border-violet-800/40 rounded px-1.5 py-0.5 leading-none">⚡ Orchestrator</span>
            )}
            {task.task_type === 'subtask' && (
              <span className="text-[10px] font-medium text-sky-400 bg-sky-900/30 border border-sky-800/40 rounded px-1.5 py-0.5 leading-none">↳ Subtask</span>
            )}
            {agent && <span className="text-xs text-slate-500">{agent.name}</span>}
            {modelInfo && (
              <span className="text-xs text-slate-600">{modelInfo.providerName} · {modelInfo.model}</span>
            )}
            <span className="text-xs text-slate-600">{timeAgo(task.created_at)}</span>
            <Badge variant={taskStatusVariant(task.status)} className="text-[10px] py-0 px-1.5">
              {taskStatusLabel(task.status)}
            </Badge>
          </div>
        </div>
        <button onClick={onClose} className="text-slate-500 hover:text-slate-300 text-lg leading-none">×</button>
      </div>

      {/* Scrollable body */}
      <div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
        {/* Task description */}
        {task.description && (
          <div>
            <p className="text-xs font-medium text-slate-400 mb-1">Task</p>
            <p className="text-sm text-slate-300 whitespace-pre-wrap">{task.description}</p>
          </div>
        )}

        {/* Output */}
        {output && (
          <div>
            <p className="text-xs font-medium text-slate-400 mb-1">Output</p>
            <MarkdownOutput content={output} className="text-sm" />
          </div>
        )}

        {task.status === 'running' && !output && (
          <p className="text-xs text-slate-500 italic">Task is running…</p>
        )}

        {/* Approval panel */}
        {task.status === 'awaiting_approval' && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 space-y-2">
            <p className="text-xs font-semibold text-amber-400">This task needs your input</p>
            <textarea
              value={approvalNote}
              onChange={e => setApprovalNote(e.target.value)}
              placeholder="Optional note to agent…"
              rows={3}
              className="w-full text-xs bg-slate-800 border border-slate-700 rounded px-2 py-1.5 text-slate-300 resize-none focus:outline-none focus:border-amber-500"
            />
            <div className="flex gap-2">
              <button
                onClick={handleApprove}
                disabled={approving}
                className="flex-1 text-xs bg-emerald-700 hover:bg-emerald-600 disabled:opacity-40 text-white rounded px-3 py-1.5"
              >
                ✓ Approve
              </button>
              <button
                onClick={handleReject}
                disabled={approving}
                className="flex-1 text-xs bg-slate-700 hover:bg-slate-600 disabled:opacity-40 text-slate-200 rounded px-3 py-1.5"
              >
                ✗ Reject
              </button>
            </div>
          </div>
        )}

        {/* AI-assisted follow-up refinement */}
        {(task.status === 'completed' || task.status === 'failed' || task.status === 'awaiting_approval') && (
          <div>
            <p className="text-xs font-medium text-slate-400 mb-2">Refine or follow up</p>
            <FollowUpThread task={task} agents={agents} onSent={onUpdated} />
          </div>
        )}
      </div>

      {/* Task action bar */}
      <div className="px-4 py-2 border-t border-slate-800 shrink-0 flex flex-wrap items-center gap-1.5">
        {(task.status === 'queued' || task.status === 'running') && (
          <button
            onClick={() => handleAction('bump')}
            disabled={!!actioning}
            className="text-[11px] px-2 py-1 rounded border border-slate-700 text-slate-400 hover:text-slate-200 hover:border-slate-500 disabled:opacity-40 transition-colors"
          >
            {actioning === 'bump' ? '…' : '⬆ Bump'}
          </button>
        )}
        {task.status === 'queued' && (
          <button
            onClick={() => handleAction('cancel')}
            disabled={!!actioning}
            className="text-[11px] px-2 py-1 rounded border border-slate-700 text-slate-400 hover:text-red-300 hover:border-red-700 disabled:opacity-40 transition-colors"
          >
            {actioning === 'cancel' ? '…' : '✕ Cancel'}
          </button>
        )}
        {task.status === 'running' && (
          <button
            onClick={() => handleAction('force-reset')}
            disabled={!!actioning}
            className="text-[11px] px-2 py-1 rounded border border-slate-700 text-slate-400 hover:text-red-300 hover:border-red-700 disabled:opacity-40 transition-colors"
          >
            {actioning === 'force-reset' ? '…' : '↺ Force Reset'}
          </button>
        )}
        {task.status === 'failed' && (
          <>
            <button
              onClick={() => handleAction('retry')}
              disabled={!!actioning}
              className="text-[11px] px-2 py-1 rounded border border-slate-700 text-slate-400 hover:text-emerald-300 hover:border-emerald-700 disabled:opacity-40 transition-colors"
            >
              {actioning === 'retry' ? '…' : '↻ Retry'}
            </button>
            <button
              onClick={() => setShowEditRetry(true)}
              disabled={!!actioning}
              className="text-[11px] px-2 py-1 rounded border border-slate-700 text-slate-400 hover:text-violet-300 hover:border-violet-700 disabled:opacity-40 transition-colors"
            >
              ✎ Edit &amp; Retry
            </button>
          </>
        )}
        {(task.status === 'completed' || task.status === 'failed') && (
          <button
            onClick={() => handleAction('dismiss')}
            disabled={!!actioning}
            className="text-[11px] px-2 py-1 rounded border border-slate-700 text-slate-400 hover:text-slate-200 hover:border-slate-500 disabled:opacity-40 transition-colors"
          >
            {actioning === 'dismiss' ? '…' : 'Dismiss'}
          </button>
        )}
      </div>

      {showEditRetry && (
        <EditRetryModal
          task={task}
          onClose={() => setShowEditRetry(false)}
          onDone={() => { setShowEditRetry(false); onUpdated() }}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// FILE PREVIEW VIEW
// ---------------------------------------------------------------------------

interface FilePreviewViewProps {
  entry: ProjectFileEntry
  content: string
  truncated: boolean
  onClose: () => void
}

function FilePreviewView({ entry, content, truncated, onClose }: FilePreviewViewProps) {
  const isMarkdown = entry.ext === 'md' || entry.ext === 'markdown'
  const isHtml = entry.ext === 'html' || entry.ext === 'htm'

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-3 border-b border-slate-800 shrink-0">
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium text-white truncate">{entry.name}</p>
          <p className="text-xs text-slate-500 truncate mt-0.5">{entry.rel_path}</p>
        </div>
        <div className="flex items-center gap-2 shrink-0 text-xs text-slate-500">
          <span>{formatBytes(entry.size_bytes)}</span>
          <span>{timeAgo(entry.modified_at)}</span>
        </div>
        <button onClick={onClose} className="text-slate-500 hover:text-slate-300 text-lg leading-none ml-2">×</button>
      </div>

      {truncated && (
        <div className="px-4 py-1.5 text-[11px] text-amber-400 bg-amber-500/10 border-b border-amber-500/20 shrink-0">
          File truncated — showing first 256 KB
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-4 py-4">
        {isMarkdown ? (
          <MarkdownOutput content={content} className="text-sm" />
        ) : isHtml ? (
          <pre className="text-xs font-mono text-slate-300 whitespace-pre-wrap break-all">{content}</pre>
        ) : (
          <pre className="text-xs font-mono text-slate-300 whitespace-pre-wrap break-all">{content}</pre>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// TASK COMPOSE FORM
// ---------------------------------------------------------------------------

interface TaskComposeFormProps {
  project: Project
  projectAgents: Agent[]
  tasks: Task[]
  onCreated: () => void
  onCancel: () => void
}

// applyTemplateVars replaces {{date}} and {{project_name}} in a string.
function applyTemplateVars(text: string, vars: Record<string, string>): string {
  return text.replace(/\{\{(\w+)\}\}/g, (_, key) => vars[key] ?? `{{${key}}}`)
}

function TaskComposeForm({ project, projectAgents, tasks, onCreated, onCancel }: TaskComposeFormProps) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [agentId, setAgentId] = useState(projectAgents[0]?.id ?? '')
  const [criticOn, setCriticOn] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [providers, setProviders] = useState<Provider[]>([])
  const [showAI, setShowAI] = useState(false)
  const [aiHint, setAiHint] = useState('')
  const [aiProviderID, setAiProviderID] = useState('')
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiError, setAiError] = useState('')
  const [templates, setTemplates] = useState<TaskTemplate[]>([])
  const [savingTemplate, setSavingTemplate] = useState(false)
  const [templateName, setTemplateName] = useState('')
  const [templateScope, setTemplateScope] = useState<'global' | 'project'>('global')
  const [showSaveTemplate, setShowSaveTemplate] = useState(false)
  const [templateSaved, setTemplateSaved] = useState(false)
  const [orchestrationEnabled, setOrchestrationEnabled] = useState(false)

  // Cost estimate
  type EstimateResult = {
    supported: boolean
    prompt_tokens: number
    estimated_output_tokens: { low: number; high: number }
    estimated_cost_usd: { low: number; high: number }
    provider: { type: string; model: string }
  }
  const [estimating, setEstimating] = useState(false)
  const [estimateResult, setEstimateResult] = useState<EstimateResult | null>(null)

  // Task dependency chain
  const [dependsOn, setDependsOn] = useState<string[]>([])
  const selectableTasks = tasks.filter(t => t.status === 'completed' || t.status === 'queued')

  const templateVars = {
    date: new Date().toISOString().slice(0, 10),
    project_name: project.name,
  }

  useEffect(() => {
    api.providers.list().then(list => {
      setProviders(list)
      setAiProviderID(list.find(p => p.type === 'llm')?.id ?? list[0]?.id ?? '')
    }).catch(() => {})
    api.taskTemplates.list(project.id).then(setTemplates).catch(() => {})
    api.admin.getSettings().then(s => setOrchestrationEnabled(!!s.dynamic_orchestration_enabled)).catch(() => {})
  }, [project.id])

  const applyTemplate = (t: TaskTemplate) => {
    setTitle(applyTemplateVars(t.title, templateVars))
    setDescription(applyTemplateVars(t.body, templateVars))
    if (t.agent_id) {
      const agent = projectAgents.find(a => a.id === t.agent_id)
      if (agent) setAgentId(agent.id)
    }
  }

  const saveAsTemplate = async () => {
    if (!templateName.trim()) return
    setSavingTemplate(true)
    try {
      await api.taskTemplates.create({
        name: templateName.trim(),
        title: title.trim() || 'Untitled',
        body: description,
        project_id: templateScope === 'project' ? project.id : null,
        agent_id: agentId || null,
      })
      setShowSaveTemplate(false)
      setTemplateName('')
      setTemplateSaved(true)
      setTimeout(() => setTemplateSaved(false), 2500)
      api.taskTemplates.list(project.id).then(setTemplates).catch(() => {})
    } catch { /* ignore */ } finally {
      setSavingTemplate(false)
    }
  }

  const generateDescription = async () => {
    if (!title.trim()) { setAiError('Enter a task title first'); return }
    setAiGenerating(true)
    setAiError('')
    try {
      const result = await api.tasks.generateDescription(title, aiHint, aiProviderID)
      setDescription(result.description)
      setShowAI(false)
      setAiHint('')
    } catch (e: unknown) {
      setAiError(e instanceof Error ? e.message : 'Generation failed')
    } finally {
      setAiGenerating(false)
    }
  }

  const estimateTask = async () => {
    if (!agentId) return
    setEstimating(true)
    setEstimateResult(null)
    try {
      const res = await api.tasks.estimate({ agent_id: agentId, title: title.trim(), description: description.trim() })
      setEstimateResult(res)
    } catch { /* ignore */ } finally {
      setEstimating(false)
    }
  }

  const handleSubmit = async () => {
    if (!title.trim()) { setError('Title is required'); return }
    if (!agentId && !orchestrationEnabled) { setError('Select an agent, or enable Dynamic Orchestration in Settings → Orchestration'); return }
    setError('')
    setSubmitting(true)
    try {
      await api.tasks.create({
        project_id: project.id,
        agent_id: agentId || '',
        title: title.trim(),
        description: description.trim(),
        critic_mode: criticOn ? 'builtin' : 'none',
        depends_on: dependsOn.length > 0 ? dependsOn : undefined,
      })
      onCreated()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to create task')
      setSubmitting(false)
    }
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-800 shrink-0">
        <h3 className="text-sm font-semibold text-white">New Task</h3>
        <button onClick={onCancel} className="text-slate-500 hover:text-slate-300 text-lg leading-none">×</button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
        {/* Agent picker (project roster only) */}
        <div>
          <label className="block text-xs font-medium text-slate-400 mb-1">Agent</label>
          {projectAgents.length === 0 && !orchestrationEnabled ? (
            <p className="text-xs text-amber-400">No agents assigned to this project. Add one via "+ Assign".</p>
          ) : (
            <select
              value={agentId}
              onChange={e => setAgentId(e.target.value)}
              className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500"
            >
              {orchestrationEnabled && (
                <option value="">★ Orchestrator will assign automatically</option>
              )}
              {projectAgents.map(a => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
          )}
          {!agentId && orchestrationEnabled && (
            <p className="text-xs text-violet-400 mt-1">
              The orchestrator will analyse this task, select the best model, and optionally decompose it into subtasks.
            </p>
          )}
        </div>

        {/* Template picker */}
        {templates.length > 0 && (
          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1">Use template</label>
            <select
              defaultValue=""
              onChange={e => {
                const t = templates.find(t => t.id === e.target.value)
                if (t) applyTemplate(t)
                e.target.value = ''
              }}
              className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500"
            >
              <option value="" disabled>Pick a template…</option>
              {templates.map(t => (
                <option key={t.id} value={t.id}>
                  {t.name}{t.project_id ? '' : ' (global)'}
                </option>
              ))}
            </select>
          </div>
        )}

        {/* Title */}
        <div>
          <label className="block text-xs font-medium text-slate-400 mb-1">Title</label>
          <input
            type="text"
            value={title}
            onChange={e => setTitle(e.target.value)}
            placeholder="What needs to be done?"
            className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500"
          />
        </div>

        {/* Description */}
        <div>
          <div className="flex items-center justify-between mb-1">
            <label className="block text-xs font-medium text-slate-400">Description <span className="text-slate-600">(optional)</span></label>
            {providers.length > 0 && (
              <button
                type="button"
                onClick={() => { setShowAI(v => !v); setAiError('') }}
                className="text-xs text-violet-400 hover:text-violet-300 transition-colors"
              >
                ✦ {showAI ? 'Hide AI assist' : 'Generate with AI'}
              </button>
            )}
          </div>
          {showAI && (
            <div className="mb-2 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-2">
              <p className="text-xs text-slate-400">Describe what the agent should do and AI will write the task description.</p>
              {providers.length > 1 && (
                <select
                  value={aiProviderID}
                  onChange={e => setAiProviderID(e.target.value)}
                  className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1.5 focus:outline-none focus:border-violet-500"
                >
                  {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              )}
              <textarea
                value={aiHint}
                onChange={e => setAiHint(e.target.value)}
                rows={2}
                placeholder="Additional context (optional)…"
                className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1.5 resize-none focus:outline-none focus:border-violet-500"
              />
              {aiError && <p className="text-xs text-red-400">{aiError}</p>}
              <div className="flex justify-end">
                <button
                  onClick={generateDescription}
                  disabled={aiGenerating}
                  className="text-xs bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-3 py-1.5"
                >
                  {aiGenerating ? 'Generating…' : '✦ Generate'}
                </button>
              </div>
            </div>
          )}
          <textarea
            value={description}
            onChange={e => setDescription(e.target.value)}
            placeholder="Provide additional context, constraints, or requirements…"
            rows={5}
            className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 resize-none focus:outline-none focus:border-violet-500"
          />
          {/* Save as template inline UI */}
          <div className="mt-1.5">
            {templateSaved ? (
              <p className="text-xs text-emerald-400">✓ Template saved</p>
            ) : showSaveTemplate ? (
              <div className="mt-2 rounded-lg border border-slate-700 bg-slate-800/60 p-3 space-y-2">
                <input
                  type="text"
                  value={templateName}
                  onChange={e => setTemplateName(e.target.value)}
                  placeholder="Template name…"
                  className="w-full text-xs bg-slate-900 border border-slate-700 text-slate-300 rounded px-2 py-1.5 focus:outline-none focus:border-violet-500"
                  autoFocus
                />
                <div className="flex items-center gap-2">
                  <label className="text-xs text-slate-400">Scope:</label>
                  <select
                    value={templateScope}
                    onChange={e => setTemplateScope(e.target.value as 'global' | 'project')}
                    className="text-xs bg-slate-900 border border-slate-700 text-slate-300 rounded px-2 py-1 focus:outline-none focus:border-violet-500"
                  >
                    <option value="global">Global (all projects)</option>
                    <option value="project">This project only</option>
                  </select>
                </div>
                <div className="flex gap-2 justify-end">
                  <button
                    onClick={() => { setShowSaveTemplate(false); setTemplateName('') }}
                    className="text-xs text-slate-500 hover:text-slate-300"
                  >Cancel</button>
                  <button
                    onClick={saveAsTemplate}
                    disabled={savingTemplate || !templateName.trim()}
                    className="text-xs bg-slate-700 hover:bg-slate-600 disabled:opacity-40 text-slate-200 rounded px-3 py-1"
                  >{savingTemplate ? 'Saving…' : '💾 Save'}</button>
                </div>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => setShowSaveTemplate(true)}
                className="text-xs text-slate-500 hover:text-slate-400 transition-colors"
              >
                💾 Save as template
              </button>
            )}
          </div>
        </div>

        {/* Depends-on picker */}
        {selectableTasks.length > 0 && (
          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1">
              Depends on <span className="text-slate-600">(optional — task starts after these complete)</span>
            </label>
            <select
              multiple
              value={dependsOn}
              onChange={e => {
                const selected = Array.from(e.target.selectedOptions, o => o.value)
                setDependsOn(selected)
              }}
              className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1.5 focus:outline-none focus:border-violet-500 min-h-[60px] max-h-[100px]"
            >
              {selectableTasks.map(t => (
                <option key={t.id} value={t.id}>
                  [{t.status === 'completed' ? '✓' : '…'}] {t.title || 'Untitled'}
                </option>
              ))}
            </select>
            {dependsOn.length > 0 && (
              <button
                type="button"
                onClick={() => setDependsOn([])}
                className="mt-1 text-[10px] text-slate-500 hover:text-slate-400"
              >
                Clear dependencies
              </button>
            )}
          </div>
        )}

        {/* Cost estimate */}
        <div>
          <div className="flex items-center justify-between">
            <span className="text-xs text-slate-500">Want to estimate cost before running?</span>
            <button
              type="button"
              onClick={estimateTask}
              disabled={estimating || !agentId}
              className="text-xs text-slate-400 hover:text-slate-200 disabled:opacity-40 transition-colors"
            >
              {estimating ? 'Estimating…' : '≈ Estimate cost'}
            </button>
          </div>
          {estimateResult && (
            <div className="mt-1.5 rounded-lg border border-slate-700 bg-slate-800/60 px-3 py-2 text-xs space-y-0.5">
              {estimateResult.supported ? (
                <>
                  <p className="text-slate-300">
                    Cost: <span className="text-emerald-400 font-medium">
                      {formatCost(estimateResult.estimated_cost_usd.low)}–{formatCost(estimateResult.estimated_cost_usd.high)}
                    </span>
                  </p>
                  <p className="text-slate-500">
                    ~{estimateResult.prompt_tokens.toLocaleString()} prompt tokens
                    · {estimateResult.estimated_output_tokens.low.toLocaleString()}–{estimateResult.estimated_output_tokens.high.toLocaleString()} output tokens
                    · {estimateResult.provider.model || estimateResult.provider.type}
                  </p>
                </>
              ) : (
                <p className="text-slate-500">Pricing not available for this provider/model.</p>
              )}
            </div>
          )}
        </div>

        {/* Devil's Advocate toggle */}
        <div className="flex items-center justify-between rounded-lg bg-slate-800 border border-slate-700 px-3 py-2.5">
          <div>
            <p className="text-xs font-medium text-slate-300">🔍 Devil's Advocate</p>
            <p className="text-[11px] text-slate-500 mt-0.5">A critic will review the output and challenge assumptions</p>
          </div>
          <button
            onClick={() => setCriticOn(v => !v)}
            className={cn(
              'relative w-9 h-5 rounded-full transition-colors shrink-0',
              criticOn ? 'bg-violet-600' : 'bg-slate-600'
            )}
          >
            <span className={cn(
              'absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform',
              criticOn ? 'left-4' : 'left-0.5'
            )} />
          </button>
        </div>

        {error && <p className="text-xs text-red-400">{error}</p>}
      </div>

      <div className="px-4 py-3 border-t border-slate-800 shrink-0 flex gap-2">
        <button
          onClick={onCancel}
          className="flex-1 text-sm border border-slate-700 text-slate-400 hover:text-slate-200 rounded px-4 py-2"
        >
          Cancel
        </button>
        <button
          onClick={handleSubmit}
          disabled={submitting || (projectAgents.length === 0 && !orchestrationEnabled)}
          className="flex-1 text-sm bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-4 py-2"
        >
          {submitting ? 'Creating…' : 'Create Task'}
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// NEW PROJECT FORM
// ---------------------------------------------------------------------------

interface NewProjectFormProps {
  allAgents: Agent[]
  onCreated: (id: string) => void
  onCancel: () => void
}

function NewProjectForm({ allAgents, onCreated, onCancel }: NewProjectFormProps) {
  const [name, setName] = useState('')
  const [objective, setObjective] = useState('')
  const [workingDir, setWorkingDir] = useState('')
  const [contextSummarisation, setContextSummarisation] = useState(false)
  const [selectedAgents, setSelectedAgents] = useState<string[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [providers, setProviders] = useState<Provider[]>([])
  const [showObjAI, setShowObjAI] = useState(false)
  const [objAIHint, setObjAIHint] = useState('')
  const [objAIProviderID, setObjAIProviderID] = useState('')
  const [objAIGenerating, setObjAIGenerating] = useState(false)

  useEffect(() => {
    api.providers.list().then(list => {
      setProviders(list)
      setObjAIProviderID(list.find(p => p.type === 'llm')?.id ?? list[0]?.id ?? '')
    }).catch(() => {})
  }, [])

  const generateObjective = async () => {
    if (!name.trim()) return
    setObjAIGenerating(true)
    try {
      const result = await api.projects.generateDescription(name, objAIHint, objAIProviderID)
      setObjective(result.description)
      setShowObjAI(false)
      setObjAIHint('')
    } catch { /* ignore */ } finally {
      setObjAIGenerating(false)
    }
  }

  const toggleAgent = (id: string) =>
    setSelectedAgents(prev => prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id])

  const handleSubmit = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setError('')
    setSubmitting(true)
    try {
      const project = await api.projects.create({
        name: name.trim(),
        objective: objective.trim(),
        working_dir: workingDir.trim(),
        kind: 'project',
        context_summarisation: contextSummarisation,
      })
      // Assign selected agents
      await Promise.all(selectedAgents.map(aid => api.projects.assignAgent(project.id, aid)))
      onCreated(project.id)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to create project')
      setSubmitting(false)
    }
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-800 shrink-0">
        <h3 className="text-sm font-semibold text-white">New Project</h3>
        <button onClick={onCancel} className="text-slate-500 hover:text-slate-300 text-lg leading-none">×</button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
        <div>
          <label className="block text-xs font-medium text-slate-400 mb-1">Name</label>
          <input
            type="text"
            value={name}
            onChange={e => setName(e.target.value)}
            placeholder="Project name"
            className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500"
          />
        </div>

        <div>
          <div className="flex items-center justify-between mb-1">
            <label className="block text-xs font-medium text-slate-400">Objective <span className="text-slate-600">(optional — injected into every task)</span></label>
            {providers.length > 0 && (
              <button
                type="button"
                onClick={() => { setShowObjAI(v => !v) }}
                className="text-xs text-violet-400 hover:text-violet-300 transition-colors"
              >
                ✦ {showObjAI ? 'Hide AI assist' : 'Generate with AI'}
              </button>
            )}
          </div>
          {showObjAI && (
            <div className="mb-2 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-2">
              <p className="text-xs text-slate-400">Describe what you want this project to accomplish and AI will write the objective.</p>
              {providers.length > 1 && (
                <select
                  value={objAIProviderID}
                  onChange={e => setObjAIProviderID(e.target.value)}
                  className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1.5 focus:outline-none focus:border-violet-500"
                >
                  {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              )}
              <textarea
                value={objAIHint}
                onChange={e => setObjAIHint(e.target.value)}
                rows={2}
                placeholder="Additional context (optional)…"
                className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1.5 resize-none focus:outline-none focus:border-violet-500"
              />
              <div className="flex justify-end">
                <button
                  onClick={generateObjective}
                  disabled={objAIGenerating || !name.trim()}
                  className="text-xs bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-3 py-1.5"
                >
                  {objAIGenerating ? 'Generating…' : '✦ Generate'}
                </button>
              </div>
            </div>
          )}
          <textarea
            value={objective}
            onChange={e => setObjective(e.target.value)}
            placeholder="What is this project trying to achieve? e.g. 'Produce a weekly market intelligence briefing covering cloud infrastructure trends'"
            rows={3}
            className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 resize-none focus:outline-none focus:border-violet-500"
          />
        </div>

        <div>
          <label className="block text-xs font-medium text-slate-400 mb-1">Working directory <span className="text-slate-600">(optional)</span></label>
          <WorkingDirInput
            id="new-proj-wdir"
            value={workingDir}
            onChange={setWorkingDir}
            placeholder="/path/to/project"
          />
        </div>

        <div className="flex items-start gap-3">
          <input
            id="new-ctx-summ"
            type="checkbox"
            checked={contextSummarisation}
            onChange={e => setContextSummarisation(e.target.checked)}
            className="mt-0.5 accent-violet-500"
          />
          <label htmlFor="new-ctx-summ" className="cursor-pointer">
            <span className="text-xs font-medium text-slate-300">Context summarisation</span>
            <p className="text-xs text-slate-500 mt-0.5">
              Summarise older follow-up turns when a chain exceeds 2 messages and ~8 000 chars, reducing input token costs by 50–80%.
            </p>
          </label>
        </div>

        {allAgents.length > 0 && (
          <div>
            <label className="block text-xs font-medium text-slate-400 mb-2">Assign agents <span className="text-slate-600">(optional)</span></label>
            <div className="space-y-1.5 max-h-48 overflow-y-auto">
              {allAgents.map(a => (
                <label key={a.id} className="flex items-center gap-2 cursor-pointer group">
                  <input
                    type="checkbox"
                    checked={selectedAgents.includes(a.id)}
                    onChange={() => toggleAgent(a.id)}
                    className="rounded border-slate-600 bg-slate-800 accent-violet-500"
                  />
                  <span className="text-sm text-slate-300 group-hover:text-white">{a.name}</span>
                </label>
              ))}
            </div>
          </div>
        )}

        {error && <p className="text-xs text-red-400">{error}</p>}
      </div>

      <div className="px-4 py-3 border-t border-slate-800 shrink-0 flex gap-2">
        <button
          onClick={onCancel}
          className="flex-1 text-sm border border-slate-700 text-slate-400 hover:text-slate-200 rounded px-4 py-2"
        >
          Cancel
        </button>
        <button
          onClick={handleSubmit}
          disabled={submitting}
          className="flex-1 text-sm bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-4 py-2"
        >
          {submitting ? 'Creating…' : 'Create Project'}
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// EditProjectForm
// ---------------------------------------------------------------------------

interface EditProjectFormProps {
  project: Project
  onSaved: () => void
  onRemoved: () => void
  onCancel: () => void
}

function EditProjectForm({ project, onSaved, onRemoved, onCancel }: EditProjectFormProps) {
  const [name, setName] = useState(project.name)
  const [objective, setObjective] = useState(project.objective ?? '')
  const [workingDir, setWorkingDir] = useState(project.working_dir ?? '')
  const [contextSummarisation, setContextSummarisation] = useState(project.context_summarisation ?? false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [confirmAction, setConfirmAction] = useState<'archive' | 'delete' | null>(null)
  const [editProviders, setEditProviders] = useState<Provider[]>([])
  const [showObjAI, setShowObjAI] = useState(false)
  const [objAIHint, setObjAIHint] = useState('')
  const [objAIProviderID, setObjAIProviderID] = useState('')
  const [objAIGenerating, setObjAIGenerating] = useState(false)

  useEffect(() => {
    api.providers.list().then(list => {
      setEditProviders(list)
      setObjAIProviderID(list.find(p => p.type === 'llm')?.id ?? list[0]?.id ?? '')
    }).catch(() => {})
  }, [])

  const generateObjective = async () => {
    setObjAIGenerating(true)
    try {
      const result = await api.projects.generateDescription(name || project.name, objAIHint, objAIProviderID)
      setObjective(result.description)
      setShowObjAI(false)
      setObjAIHint('')
    } catch { /* ignore */ } finally {
      setObjAIGenerating(false)
    }
  }

  const handleSubmit = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setError('')
    setSubmitting(true)
    try {
      await api.projects.update(project.id, {
        name: name.trim(),
        objective: objective.trim(),
        working_dir: workingDir.trim(),
        context_summarisation: contextSummarisation,
      })
      onSaved()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to save project')
      setSubmitting(false)
    }
  }

  const handleArchive = async () => {
    setSubmitting(true)
    try {
      await api.projects.archive(project.id)
      onRemoved()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to archive project')
      setSubmitting(false)
      setConfirmAction(null)
    }
  }

  const handleDelete = async () => {
    setSubmitting(true)
    try {
      await api.projects.delete(project.id)
      onRemoved()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to delete project')
      setSubmitting(false)
      setConfirmAction(null)
    }
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-800 shrink-0">
        <h3 className="text-sm font-semibold text-white">Edit Project</h3>
        <button onClick={onCancel} className="text-slate-500 hover:text-slate-300 text-lg leading-none">×</button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
        <div>
          <label className="block text-xs font-medium text-slate-400 mb-1">Name</label>
          <input
            type="text"
            value={name}
            onChange={e => setName(e.target.value)}
            placeholder="Project name"
            className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500"
          />
        </div>

        <div>
          <div className="flex items-center justify-between mb-1">
            <label className="block text-xs font-medium text-slate-400">Objective <span className="text-slate-600">(optional — injected into every task)</span></label>
            {editProviders.length > 0 && (
              <button
                type="button"
                onClick={() => setShowObjAI(v => !v)}
                className="text-xs text-violet-400 hover:text-violet-300 transition-colors"
              >
                ✦ {showObjAI ? 'Hide AI assist' : 'Generate with AI'}
              </button>
            )}
          </div>
          {showObjAI && (
            <div className="mb-2 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-2">
              <p className="text-xs text-slate-400">Describe what you want this project to accomplish and AI will write the objective.</p>
              {editProviders.length > 1 && (
                <select
                  value={objAIProviderID}
                  onChange={e => setObjAIProviderID(e.target.value)}
                  className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1.5 focus:outline-none focus:border-violet-500"
                >
                  {editProviders.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              )}
              <textarea
                value={objAIHint}
                onChange={e => setObjAIHint(e.target.value)}
                rows={2}
                placeholder="Additional context (optional)…"
                className="w-full text-xs bg-slate-800 border border-slate-700 text-slate-300 rounded px-2 py-1.5 resize-none focus:outline-none focus:border-violet-500"
              />
              <div className="flex justify-end">
                <button
                  onClick={generateObjective}
                  disabled={objAIGenerating}
                  className="text-xs bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-3 py-1.5"
                >
                  {objAIGenerating ? 'Generating…' : '✦ Generate'}
                </button>
              </div>
            </div>
          )}
          <textarea
            value={objective}
            onChange={e => setObjective(e.target.value)}
            placeholder="What is this project trying to achieve?"
            rows={3}
            className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 resize-none focus:outline-none focus:border-violet-500"
          />
        </div>

        <div>
          <label className="block text-xs font-medium text-slate-400 mb-1">Working directory <span className="text-slate-600">(optional)</span></label>
          <input
            type="text"
            value={workingDir}
            onChange={e => setWorkingDir(e.target.value)}
            placeholder="/path/to/project"
            className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500 font-mono"
          />
        </div>

        <div className="flex items-start gap-3">
          <input
            id="ctx-summ"
            type="checkbox"
            checked={contextSummarisation}
            onChange={e => setContextSummarisation(e.target.checked)}
            className="mt-0.5 accent-violet-500"
          />
          <label htmlFor="ctx-summ" className="cursor-pointer">
            <span className="text-xs font-medium text-slate-300">Context summarisation</span>
            <p className="text-xs text-slate-500 mt-0.5">
              Summarise older follow-up turns when a chain exceeds 2 messages and ~8 000 chars, reducing input token costs by 50–80%.
            </p>
          </label>
        </div>

        {error && <p className="text-xs text-red-400">{error}</p>}

        {/* Danger zone */}
        <div className="pt-2 border-t border-slate-800 space-y-2">
          <p className="text-xs font-medium text-slate-500 uppercase tracking-wide">Danger zone</p>

          {confirmAction === 'archive' ? (
            <div className="rounded border border-amber-800 bg-amber-950/40 p-3 space-y-2">
              <p className="text-xs text-amber-300">Archive this project? It will be hidden from the main list but can be restored later.</p>
              <div className="flex gap-2">
                <button onClick={() => setConfirmAction(null)} className="flex-1 text-xs border border-slate-700 text-slate-400 hover:text-slate-200 rounded px-3 py-1.5">Cancel</button>
                <button onClick={handleArchive} disabled={submitting} className="flex-1 text-xs bg-amber-700 hover:bg-amber-600 disabled:opacity-40 text-white rounded px-3 py-1.5">Confirm Archive</button>
              </div>
            </div>
          ) : (
            <button
              onClick={() => setConfirmAction('archive')}
              className="w-full text-xs border border-slate-700 text-amber-500 hover:text-amber-300 hover:border-amber-700 rounded px-3 py-1.5 text-left"
            >
              Archive project…
            </button>
          )}

          {confirmAction === 'delete' ? (
            <div className="rounded border border-red-800 bg-red-950/40 p-3 space-y-2">
              <p className="text-xs text-red-300">Permanently delete <strong className="text-red-200">{project.name}</strong>? This cannot be undone.</p>
              <div className="flex gap-2">
                <button onClick={() => setConfirmAction(null)} className="flex-1 text-xs border border-slate-700 text-slate-400 hover:text-slate-200 rounded px-3 py-1.5">Cancel</button>
                <button onClick={handleDelete} disabled={submitting} className="flex-1 text-xs bg-red-700 hover:bg-red-600 disabled:opacity-40 text-white rounded px-3 py-1.5">Delete Forever</button>
              </div>
            </div>
          ) : (
            <button
              onClick={() => setConfirmAction('delete')}
              className="w-full text-xs border border-slate-700 text-red-500 hover:text-red-300 hover:border-red-700 rounded px-3 py-1.5 text-left"
            >
              Delete project…
            </button>
          )}
        </div>
      </div>

      <div className="px-4 py-3 border-t border-slate-800 shrink-0 flex gap-2">
        <button
          onClick={onCancel}
          className="flex-1 text-sm border border-slate-700 text-slate-400 hover:text-slate-200 rounded px-4 py-2"
        >
          Cancel
        </button>
        <button
          onClick={handleSubmit}
          disabled={submitting}
          className="flex-1 text-sm bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-4 py-2"
        >
          {submitting ? 'Saving…' : 'Save Changes'}
        </button>
      </div>
    </div>
  )
}
