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
  persona: string
  instructions: string
  guardrails: string
  provider_id: string
  model_override: string
  can_spawn_agents: boolean
  heartbeat_interval: number | null
  status: 'active' | 'paused' | 'disabled'
  created_at: string
}

export interface GeneratedAgent {
  persona: string
  instructions: string
  guardrails: string
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
  owner: string
  status: 'active' | 'archived'
  created_at: string
}

export interface Task {
  id: string
  project_id: string
  agent_id: string
  parent_task_id: string | null
  title: string
  description: string
  status: 'pending' | 'queued' | 'running' | 'completed' | 'failed' | 'awaiting_approval'
  input: string
  output: string
  cost_usd: number
  created_at: string
  started_at: string | null
  completed_at: string | null
}

export interface CostSummary {
  id: string
  name: string
  total_cost_usd: number
}

export interface DailyCost {
  date: string
  cost_usd: number
}

export interface TaskStatusCount {
  status: string
  count: number
}

export interface CostsResponse {
  total_cost_usd: number
  total_tasks: number
  by_agent: CostSummary[]
  by_project: CostSummary[]
  by_day: DailyCost[]
  by_status: TaskStatusCount[]
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
    get: (id: string) => request<Provider>(`/providers/${id}`),
    create: (data: Partial<Provider>) => request<Provider>('/providers', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<Provider>) => request<Provider>(`/providers/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/providers/${id}`, { method: 'DELETE' }),
  },
  agents: {
    list: () => request<Agent[]>('/agents'),
    get: (id: string) => request<Agent>(`/agents/${id}`),
    create: (data: Partial<Agent>) => request<Agent>('/agents', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<Agent>) => request<Agent>(`/agents/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/agents/${id}`, { method: 'DELETE' }),
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
    exportUrl: (id: string) => `/api/teams/${id}/export`,
    import: (bundle: unknown, apiKeys: Record<string, string>) =>
      request<{ team_id: string; team_name: string; agent_ids: string[]; provider_ids: string[]; skipped: string[] }>(
        '/import/team', { method: 'POST', body: JSON.stringify({ bundle, api_keys: apiKeys }) }
      ),
  },
  projects: {
    list: () => request<Project[]>('/projects'),
    get: (id: string) => request<Project>(`/projects/${id}`),
    create: (data: Partial<Project>) => request<Project>('/projects', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<Project>) => request<Project>(`/projects/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/projects/${id}`, { method: 'DELETE' }),
    listAgents: (id: string) => request<Agent[]>(`/projects/${id}/agents`),
    assignAgent: (id: string, agentId: string) => request<void>(`/projects/${id}/agents`, { method: 'POST', body: JSON.stringify({ agent_id: agentId }) }),
    removeAgent: (id: string, agentId: string) => request<void>(`/projects/${id}/agents/${agentId}`, { method: 'DELETE' }),
    assignTeam: (id: string, teamId: string) => request<{ assigned: number, total: number, team: string }>(`/projects/${id}/teams`, { method: 'POST', body: JSON.stringify({ team_id: teamId }) }),
  },
  tasks: {
    list: (projectId: string) => request<Task[]>(`/tasks?project_id=${projectId}`),
    listAll: () => request<Task[]>('/tasks'),
    get: (id: string) => request<Task>(`/tasks/${id}`),
    create: (data: Partial<Task>) => request<Task>('/tasks', { method: 'POST', body: JSON.stringify(data) }),
    delete: (id: string) => request<void>(`/tasks/${id}`, { method: 'DELETE' }),
    retry: (id: string) => request<Task>(`/tasks/${id}/retry`, { method: 'POST', body: '{}' }),
    dismiss: (id: string) => request<Task>(`/tasks/${id}/dismiss`, { method: 'POST', body: '{}' }),
    update: (id: string, data: { title?: string; description?: string }) =>
      request<Task>(`/tasks/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    listRunning: () => request<Task[]>('/tasks/running'),
  },
  inbox: {
    list: () => request<Task[]>('/inbox'),
    // All tasks needing attention: failed + awaiting_approval, across all projects
    listAttention: () => request<Task[]>('/tasks/attention'),
    approve: (taskId: string) => request<Task>(`/inbox/${taskId}/approve`, { method: 'POST', body: '{}' }),
    reject: (taskId: string) => request<Task>(`/inbox/${taskId}/reject`, { method: 'POST', body: '{}' }),
    revise: (taskId: string, feedback: string) => request<Task>(`/inbox/${taskId}/revise`, { method: 'POST', body: JSON.stringify({ feedback }) }),
  },
  stats: {
    costs: () => request<CostsResponse>('/stats/costs'),
  },
}
