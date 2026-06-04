// Typed API client for Phoenix backend

export interface Provider {
  id: string
  name: string
  type: 'llm' | 'coding_agent'
  config: string
  created_by: string
  created_at: string
}

export interface Agent {
  id: string
  name: string
  behaviour: string
  persona: string        // legacy
  instructions: string   // legacy
  guardrails: string
  hard_guardrails: string
  provider_id: string
  model_override: string
  can_spawn_agents: boolean
  can_hire_agents: boolean
  max_concurrent: number
  status: 'active' | 'paused' | 'disabled'
  created_at: string
  template_id: string | null
}

export interface GeneratedAgent {
  behaviour: string
  persona: string
  instructions: string
  guardrails: string
  hard_guardrails: string
}

export interface Team {
  id: string
  name: string
  description: string
  created_by: string
  created_at: string
  agents: Agent[]
}

export interface Project {
  id: string
  name: string
  description: string
  working_dir: string
  kind: 'project' | 'monitor'
  schedule_interval: number | null  // seconds; null = no schedule (monitors only)
  owner: string
  status: 'active' | 'archived'
  critic_agent_id: string | null
  critic_mode: 'none' | 'builtin' | string  // "none" | "builtin" | "agent:<id>"
  tags: string[] | null             // free-text grouping labels (null for rows predating migration 023)
  created_at: string
}

export interface Task {
  id: string
  project_id: string
  agent_id: string
  parent_task_id: string | null
  follow_up_of: string | null
  title: string
  description: string
  status: 'pending' | 'queued' | 'running' | 'completed' | 'failed' | 'awaiting_approval'
  input: string
  output: string
  cost_usd: number
  tokens_in: number
  tokens_out: number
  source: string
  health_signal: 'all_clear' | 'needs_attention' | 'failed' | null
  guardrail_reason: string | null
  dismissed: boolean
  is_critic_review: boolean
  reviewed_task_id: string | null
  critic_mode: string  // "inherit" | "none" | "builtin" | "agent:<id>"
  created_at: string
  started_at: string | null
  completed_at: string | null
}

export interface CostSummary {
  id: string
  name: string
  total_cost_usd: number
  task_count: number
}

export interface DailyCost {
  date: string
  cost_usd: number
}

export interface TaskStatusCount {
  status: string
  count: number
}

export interface AgentDraft {
  id: string
  created_by_agent_id: string
  created_by_agent_name: string
  created_by_task_id: string | null
  created_by_task_title: string
  name: string
  persona: string
  instructions: string
  guardrails: string
  provider_id: string
  status: 'pending_approval' | 'approved' | 'rejected'
  dismissed: boolean
  created_at: string
}

export interface SystemSettings {
  global_guardrails_enabled: boolean
  global_guardrails: string
}

export interface CostsResponse {
  total_cost_usd: number
  total_tasks: number
  by_agent: CostSummary[]
  by_project: CostSummary[]
  by_day: DailyCost[]
  by_status: TaskStatusCount[]
}

export interface Memo {
  id: string
  project_id: string
  project_name: string
  task_id: string
  agent_id: string
  agent_name: string
  title: string
  body: string
  priority: 'normal' | 'high'
  status: 'unread' | 'read' | 'flagged' | 'archived'
  created_at: string
}

export interface SysInfo {
  version: string
  uptime_seconds: number
  go_version: string
  db_size_bytes: number
  db_path: string
  total_tasks: number
  task_counts: { status: string; count: number }[]
  active_tasks: number
}

const BASE = '/api'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error || res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

// Providers
export const api = {
  providers: {
    list: () => request<Provider[]>('/providers'),
    listModels: (id: string) => request<{ supported: boolean; models: string[]; error?: string }>(`/providers/${id}/models`),
    get: (id: string) => request<Provider>(`/providers/${id}`),
    create: (data: Partial<Provider>) => request<Provider>('/providers', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<Provider>) => request<Provider>(`/providers/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/providers/${id}`, { method: 'DELETE' }),
    resync: (id: string) => request<{ status: string; message: string }>(`/providers/${id}/resync`, { method: 'POST' }),
  },
  agents: {
    list: () => request<Agent[]>('/agents'),
    get: (id: string) => request<Agent>(`/agents/${id}`),
    create: (data: Partial<Agent>) => request<Agent>('/agents', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<Agent>) => request<Agent>(`/agents/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/agents/${id}`, { method: 'DELETE' }),
    export: async (id: string): Promise<Blob> => {
      const res = await fetch(`/api/agents/${id}/export`)
      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: res.statusText }))
        throw new Error(err.error || res.statusText)
      }
      return res.blob()
    },
    importAgent: async (data: { bundle: unknown; api_key?: string }) => {
      const res = await fetch('/api/agents/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      })
      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: res.statusText }))
        throw new Error(err.error || res.statusText)
      }
      return res.json() as Promise<Agent>
    },
    generate: (description: string, providerId?: string) =>
      request<GeneratedAgent>('/agents/generate', {
        method: 'POST',
        body: JSON.stringify({ description, provider_id: providerId ?? '' }),
      }),
  },
  teams: {
    list: () => request<Team[]>('/teams'),
    get: (id: string) => request<Team>(`/teams/${id}`),
    create: (data: Partial<Team> & { agent_ids?: string[] }) => request<Team>('/teams', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<Team>) => request<Team>(`/teams/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/teams/${id}`, { method: 'DELETE' }),
    addAgent: (id: string, agentId: string) => request<void>(`/teams/${id}/agents`, { method: 'POST', body: JSON.stringify({ agent_id: agentId }) }),
    removeAgent: (id: string, agentId: string) => request<void>(`/teams/${id}/agents/${agentId}`, { method: 'DELETE' }),
    broadcast: async (id: string, data: { project_id: string; title: string; description: string }) => {
      const res = await fetch(`/api/teams/${id}/broadcast`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      })
      return res.json()
    },
    exportUrl: (id: string) => `/api/teams/${id}/export`,
    generateDescription: (name: string, hint?: string, providerId?: string) =>
      request<{ description: string }>('/teams/generate-description', {
        method: 'POST',
        body: JSON.stringify({ name, hint: hint ?? '', provider_id: providerId ?? '' }),
      }),
    import: (bundle: unknown, apiKeys: Record<string, string>) =>
      request<{ team_id: string; team_name: string; agent_ids: string[]; provider_ids: string[]; skipped: string[] }>(
        '/import/team', { method: 'POST', body: JSON.stringify({ bundle, api_keys: apiKeys }) }
      ),
  },
  projects: {
    list: (kind?: 'project' | 'monitor') => request<Project[]>(kind ? `/projects?kind=${kind}` : '/projects'),
    listArchived: (kind?: 'project' | 'monitor') =>
      request<Project[]>(kind ? `/projects?status=archived&kind=${kind}` : '/projects?status=archived'),
    get: (id: string) => request<Project>(`/projects/${id}`),
    create: (data: Partial<Project>) => request<Project>('/projects', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<Project>) => request<Project>(`/projects/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/projects/${id}`, { method: 'DELETE' }),
    archive: (id: string) => request<Project>(`/projects/${id}/archive`, { method: 'POST', body: '{}' }),
    restore: (id: string) => request<Project>(`/projects/${id}/restore`, { method: 'POST', body: '{}' }),
    listAgents: (id: string) => request<Agent[]>(`/projects/${id}/agents`),
    assignAgent: (id: string, agentId: string) => request<void>(`/projects/${id}/agents`, { method: 'POST', body: JSON.stringify({ agent_id: agentId }) }),
    removeAgent: (id: string, agentId: string) => request<void>(`/projects/${id}/agents/${agentId}`, { method: 'DELETE' }),
    assignTeam: (id: string, teamId: string) => request<{ assigned: number, total: number, team: string }>(`/projects/${id}/teams`, { method: 'POST', body: JSON.stringify({ team_id: teamId }) }),
    generateDescription: (name: string, hint?: string, providerId?: string) =>
      request<{ description: string }>('/projects/generate-description', {
        method: 'POST',
        body: JSON.stringify({ name, hint: hint ?? '', provider_id: providerId ?? '' }),
      }),
  },
  tasks: {
    list: (projectId: string) => request<Task[]>(`/tasks?project_id=${projectId}`),
    listAll: () => request<Task[]>('/tasks'),
    get: (id: string) => request<Task>(`/tasks/${id}`),
    create: (data: Partial<Task>) => request<Task>('/tasks', { method: 'POST', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/tasks/${id}`, { method: 'DELETE' }),
    retry: (id: string) => request<Task>(`/tasks/${id}/retry`, { method: 'POST', body: '{}' }),
    dismiss: (id: string) => request<Task>(`/tasks/${id}/dismiss`, { method: 'POST', body: '{}' }),
    followUp: (id: string, description: string, agentId?: string) =>
      request<Task>(`/tasks/${id}/followup`, {
        method: 'POST',
        body: JSON.stringify({ description, agent_id: agentId ?? '' }),
      }),
    quick: (agentId: string, title: string, description: string) =>
      request<Task>('/tasks/quick', {
        method: 'POST',
        body: JSON.stringify({ agent_id: agentId, title, description }),
      }),
    update: (id: string, data: { title?: string; description?: string }) =>
      request<Task>(`/tasks/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    search: (q: string) => request<Task[]>(`/tasks/search?q=${encodeURIComponent(q)}`),
    estimate: (agentId: string, description: string) =>
      request<{ supported: boolean; estimated_cost_usd: number }>('/tasks/estimate', {
        method: 'POST',
        body: JSON.stringify({ agent_id: agentId, description }),
      }),
    generateDescription: (title: string, hint?: string, providerId?: string) =>
      request<{ description: string }>('/tasks/generate-description', {
        method: 'POST',
        body: JSON.stringify({ title, hint: hint ?? '', provider_id: providerId ?? '' }),
      }),
    listByAgent: (agentId: string) => request<Task[]>(`/agents/${agentId}/tasks`),
    listRunning: () => request<Task[]>('/tasks/running'),
    cancel: (id: string) => request<void>(`/tasks/${id}/cancel`, { method: 'POST', body: '{}' }),
  },
  inbox: {
    list: () => request<Task[]>('/inbox'),
    // All tasks needing attention: failed + awaiting_approval, across all projects
    listAttention: () => request<Task[]>('/tasks/attention'),
    approve: (taskId: string) => request<Task>(`/inbox/${taskId}/approve`, { method: 'POST', body: '{}' }),
    reject: (taskId: string) => request<Task>(`/inbox/${taskId}/reject`, { method: 'POST', body: '{}' }),
    revise: (taskId: string, feedback: string) => request<Task>(`/inbox/${taskId}/revise`, { method: 'POST', body: JSON.stringify({ feedback }) }),
    dismissAll: (filter: 'failed' | 'awaiting' | 'completed' | 'all' = 'all') =>
      request<{ dismissed: number }>(`/inbox/dismiss-all?filter=${filter}`, { method: 'POST', body: '{}' }),
  },
  agentDrafts: {
    list: () => request<AgentDraft[]>('/agent-drafts'),
    create: (data: Partial<AgentDraft>) => request<AgentDraft>('/agent-drafts', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<AgentDraft>) => request<AgentDraft>(`/agent-drafts/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    approve: (id: string, providerId?: string) => request<Agent>(`/agent-drafts/${id}/approve`, { method: 'POST', body: JSON.stringify({ provider_id: providerId ?? '' }) }),
    reject: (id: string) => request<void>(`/agent-drafts/${id}/reject`, { method: 'POST', body: '{}' }),
    dismiss: (id: string) => request<void>(`/agent-drafts/${id}/dismiss`, { method: 'POST', body: '{}' }),
  },
  memos: {
    list: (status?: string) => request<Memo[]>(status ? `/memos?status=${status}` : '/memos'),
    count: () => request<{ count: number }>('/memos/count'),
    create: (data: Partial<Memo>) => request<Memo>('/memos', { method: 'POST', body: JSON.stringify(data) }),
    updateStatus: (id: string, status: Memo['status']) =>
      request<Memo>(`/memos/${id}/status`, { method: 'PUT', body: JSON.stringify({ status }) }),
    delete: (id: string) => request<void>(`/memos/${id}`, { method: 'DELETE' }),
  },
  stats: {
    costs: () => request<CostsResponse>('/stats/costs'),
  },
  admin: {
    getSettings: () => request<SystemSettings>('/admin/settings'),
    saveSettings: (data: SystemSettings) => request<SystemSettings>('/admin/settings', { method: 'PUT', body: JSON.stringify(data) }),
    generateGlobalGuardrails: (description: string, providerId?: string) =>
      request<{ guardrails: string }>('/admin/settings/generate-guardrails', {
        method: 'POST',
        body: JSON.stringify({ description, provider_id: providerId ?? '' }),
      }),
    sysinfo: () => request<SysInfo>('/admin/sysinfo'),
  },
}
