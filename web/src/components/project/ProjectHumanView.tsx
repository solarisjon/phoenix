import type { Task, Agent, Project } from '../../lib/api'
import { TaskThreadCard } from './TaskThreadCard'

interface Props {
  project: Project
  tasks: Task[]
  agents: Agent[]
  onUpdate: () => void
  onNewTask: () => void
  emptyState?: React.ReactNode
}

export function ProjectHumanView({ project, tasks, agents, onUpdate, onNewTask, emptyState }: Props) {
  // Split into top-level tasks and follow-ups
  const topLevel = tasks.filter(t => !t.follow_up_of && !t.dismissed)
  const followUpMap: Record<string, Task[]> = {}
  for (const t of tasks) {
    if (t.follow_up_of) {
      if (!followUpMap[t.follow_up_of]) followUpMap[t.follow_up_of] = []
      followUpMap[t.follow_up_of].push(t)
    }
  }
  // Sort follow-ups oldest first so thread reads top-to-bottom
  for (const id in followUpMap) {
    followUpMap[id].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
  }

  const assignedAgents = agents.filter(a =>
    tasks.some(t => t.agent_id === a.id)
  )
  const agentNames = assignedAgents.map(a => a.name).join(', ') || 'No agents assigned'

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold text-stone-900">{project.name}</h1>
          <p className="text-sm text-stone-400 mt-1">{agentNames}</p>
        </div>
        <button
          onClick={onNewTask}
          className="bg-indigo-600 hover:bg-indigo-700 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors"
        >
          + New Task
        </button>
      </div>

      {/* Task thread list */}
      {topLevel.length === 0 ? (
        emptyState ?? (
          <div className="text-center py-16 text-stone-400">
            <p className="text-lg mb-2">No tasks yet</p>
            <p className="text-sm">Create your first task to get started.</p>
          </div>
        )
      ) : (
        <div className="space-y-4">
          {topLevel.map(task => (
            <TaskThreadCard
              key={task.id}
              task={task}
              followUps={followUpMap[task.id] ?? []}
              agents={agents}
              onUpdate={onUpdate}
            />
          ))}
        </div>
      )}
    </div>
  )
}
