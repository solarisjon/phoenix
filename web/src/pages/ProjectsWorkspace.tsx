/**
 * ProjectsWorkspace — three-pane email-client layout for Projects.
 *
 * LEFT:   project list with status dots
 * MIDDLE: task list (compact rows) + Files tab for selected project
 * RIGHT:  task detail / compose form / new-project form
 */

import { useState, useEffect, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, type Project, type Task, type Agent, type ProjectSummary, type ProjectFileEntry, type Provider } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { FollowUpThread } from '@/components/ui/follow-up-thread'
import { taskStatusVariant, taskStatusLabel, parseOutput, timeAgo } from '@/lib/utils'
import { cn } from '@/lib/utils'

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

  useEffect(() => { loadBase() }, [loadBase])

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
    if (projectId) {
      loadProject(projectId)
      setRightPane({ type: 'empty' })
    } else {
      setTasks([])
      setProjectAgents([])
    }
  }, [projectId, loadProject])

  // Auto-select first project when none selected and list is ready
  useEffect(() => {
    if (!loading && !projectId && projects.length > 0) {
      navigate(`/projects/${projects[0].id}`, { replace: true })
    }
  }, [loading, projectId, projects, navigate])

  const selectedProject = projects.find(p => p.id === projectId) ?? null

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
        projectAgents={projectAgents}
        allAgents={allAgents}
        tasks={tasks}
        selectedTask={rightPane.type === 'task' ? rightPane.task : null}
        selectedFile={rightPane.type === 'file' ? rightPane.entry : null}
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
      />

      {/* RIGHT PANE — detail / compose / new-project */}
      <RightPaneArea
        pane={rightPane}
        project={selectedProject}
        projectAgents={projectAgents}
        allAgents={allAgents}
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
}

function MiddlePane({
  project, projectAgents, allAgents, tasks, selectedTask, selectedFile, onTaskClick, onFileClick, onCompose, onEdit, onAgentAssigned,
}: MiddlePaneProps) {
  const [tab, setTab] = useState<'tasks' | 'files'>('tasks')
  const [assignOpen, setAssignOpen] = useState(false)
  const [assignAgentId, setAssignAgentId] = useState('')
  const [assigning, setAssigning] = useState(false)
  const [files, setFiles] = useState<ProjectFileEntry[]>([])
  const [filesLoading, setFilesLoading] = useState(false)

  const unassignedAgents = allAgents.filter(a => !projectAgents.find(pa => pa.id === a.id))

  // Load file list when Files tab is selected
  useEffect(() => {
    if (tab !== 'files' || !project) return
    setFilesLoading(true)
    api.projects.listFiles(project.id)
      .then(setFiles)
      .catch(() => setFiles([]))
      .finally(() => setFilesLoading(false))
  }, [tab, project?.id])

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

  if (!project) {
    return (
      <div className="flex-1 flex items-center justify-center text-slate-600 text-sm">
        Select a project
      </div>
    )
  }

  return (
    <div className="flex flex-col w-80 shrink-0 border-r border-slate-800 bg-slate-900">
      {/* Project header */}
      <div className="px-4 pt-3 pb-2 border-b border-slate-800">
        <div className="flex items-start justify-between gap-2">
          <h2 className="text-sm font-semibold text-white truncate">{project.name}</h2>
          <button
            onClick={onEdit}
            className="shrink-0 text-xs text-slate-500 hover:text-slate-300 transition-colors"
            title="Edit project"
          >
            ✎ Edit
          </button>
        </div>
        {project.description && (
          <p className="text-xs text-slate-500 mt-0.5 line-clamp-2">{project.description}</p>
        )}

        {/* Agent roster pills */}
        <div className="flex flex-wrap items-center gap-1.5 mt-2">
          {projectAgents.map(a => (
            <span key={a.id} className="text-xs bg-slate-800 text-slate-300 rounded-full px-2 py-0.5">
              {a.name}
            </span>
          ))}
          <button
            onClick={() => setAssignOpen(v => !v)}
            className="text-xs text-violet-400 hover:text-violet-300"
          >
            + Assign
          </button>
        </div>

        {/* Assign agent popover */}
        {assignOpen && (
          <div className="mt-2 p-2 bg-slate-800 rounded border border-slate-700 flex gap-2">
            <select
              value={assignAgentId}
              onChange={e => setAssignAgentId(e.target.value)}
              className="flex-1 text-xs bg-slate-900 border border-slate-700 text-slate-300 rounded px-2 py-1"
            >
              <option value="">Select agent…</option>
              {unassignedAgents.map(a => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
            <button
              onClick={handleAssign}
              disabled={!assignAgentId || assigning}
              className="text-xs bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded px-2 py-1"
            >
              {assigning ? '…' : 'Add'}
            </button>
          </div>
        )}
      </div>

      {/* Tabs */}
      <div className="flex items-center border-b border-slate-800 px-4">
        {(['tasks', 'files'] as const).map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={cn(
              'text-xs py-2 mr-4 border-b-2 transition-colors capitalize',
              tab === t
                ? 'border-violet-500 text-white'
                : 'border-transparent text-slate-500 hover:text-slate-300'
            )}
          >
            {t}
          </button>
        ))}
        <div className="flex-1" />
        {tab === 'tasks' && (
          <button
            onClick={onCompose}
            className="text-xs text-violet-400 hover:text-violet-300 py-2"
          >
            + Task
          </button>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto">
        {tab === 'tasks' && (
          tasks.length === 0
            ? <p className="text-xs text-slate-500 px-4 py-6">No tasks yet. Create one with + Task.</p>
            : tasks.map(task => (
                <TaskRow
                  key={task.id}
                  task={task}
                  agents={projectAgents}
                  selected={selectedTask?.id === task.id}
                  onClick={() => onTaskClick(task)}
                />
              ))
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
    </div>
  )
}

interface TaskRowProps {
  task: Task
  agents: Agent[]
  selected: boolean
  onClick: () => void
}

function TaskRow({ task, agents, selected, onClick }: TaskRowProps) {
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
    <button
      onClick={onClick}
      className={cn(
        'w-full text-left px-3 py-2.5 hover:bg-slate-800/60 transition-colors border-b border-slate-800/60 border-l-2',
        borderColor[variant] ?? 'border-l-slate-600',
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
  onClose: () => void
  onTaskCreated: () => void
  onProjectCreated: (id: string) => void
  onProjectUpdated: () => void
  onProjectRemoved: () => void
  onTaskUpdated: () => void
}

function RightPaneArea({
  pane, project, projectAgents, allAgents, onClose, onTaskCreated, onProjectCreated, onProjectUpdated, onProjectRemoved, onTaskUpdated,
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
  const output = parseOutput(task.output)
  const agent = agents.find(a => a.id === task.agent_id)

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

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="flex items-start gap-3 px-4 py-3 border-b border-slate-800 shrink-0">
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-white leading-snug">{task.title || 'Untitled task'}</h3>
          <div className="flex items-center gap-2 mt-1">
            {agent && <span className="text-xs text-slate-500">{agent.name}</span>}
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
  onCreated: () => void
  onCancel: () => void
}

function TaskComposeForm({ project, projectAgents, onCreated, onCancel }: TaskComposeFormProps) {
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

  useEffect(() => {
    api.providers.list().then(list => {
      setProviders(list)
      setAiProviderID(list.find(p => p.type === 'llm')?.id ?? list[0]?.id ?? '')
    }).catch(() => {})
  }, [])

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

  const handleSubmit = async () => {
    if (!title.trim()) { setError('Title is required'); return }
    if (!agentId) { setError('Select an agent'); return }
    setError('')
    setSubmitting(true)
    try {
      await api.tasks.create({
        project_id: project.id,
        agent_id: agentId,
        title: title.trim(),
        description: description.trim(),
        critic_mode: criticOn ? 'builtin' : 'none',
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
          {projectAgents.length === 0 ? (
            <p className="text-xs text-amber-400">No agents assigned to this project. Add one via "+ Assign".</p>
          ) : (
            <select
              value={agentId}
              onChange={e => setAgentId(e.target.value)}
              className="w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500"
            >
              {projectAgents.map(a => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
          )}
        </div>

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
          disabled={submitting || projectAgents.length === 0}
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
  const [description, setDescription] = useState('')
  const [workingDir, setWorkingDir] = useState('')
  const [selectedAgents, setSelectedAgents] = useState<string[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const toggleAgent = (id: string) =>
    setSelectedAgents(prev => prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id])

  const handleSubmit = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setError('')
    setSubmitting(true)
    try {
      const project = await api.projects.create({
        name: name.trim(),
        description: description.trim(),
        working_dir: workingDir.trim(),
        kind: 'project',
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
          <label className="block text-xs font-medium text-slate-400 mb-1">Description <span className="text-slate-600">(optional)</span></label>
          <textarea
            value={description}
            onChange={e => setDescription(e.target.value)}
            placeholder="What is this project about?"
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
  const [description, setDescription] = useState(project.description ?? '')
  const [workingDir, setWorkingDir] = useState(project.working_dir ?? '')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [confirmAction, setConfirmAction] = useState<'archive' | 'delete' | null>(null)

  const handleSubmit = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setError('')
    setSubmitting(true)
    try {
      await api.projects.update(project.id, {
        name: name.trim(),
        description: description.trim(),
        working_dir: workingDir.trim(),
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
          <label className="block text-xs font-medium text-slate-400 mb-1">Description <span className="text-slate-600">(optional)</span></label>
          <textarea
            value={description}
            onChange={e => setDescription(e.target.value)}
            placeholder="What is this project about?"
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
