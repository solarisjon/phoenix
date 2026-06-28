// Typed API client for Phoenix backend

export interface TaskTemplate {
  id: string
  name: string
  description: string
  title: string
  body: string
  project_id: string | null
  agent_id: string | null
  created_at: string
}

export interface Provider {
  id: string
  name: string
  type: 'llm' | 'coding_agent'
  config: string
  created_by: string
  created_at: string
  health_status: 'ok' | 'error' | 'unknown'
  health_latency_ms: number | null
  health_error: string
  health_checked_at: string | null
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
  max_cost_per_run: number
  fallback_model: string
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
  objective: string          // high-level goal statement; empty string = not set
  working_dir: string
  kind: 'project' | 'monitor'
  schedule_interval: number | null  // seconds; null = no schedule (monitors only)
  schedule_kind: 'interval' | 'daily'  // monitors only; 'interval' is the default/back-compat
  schedule_times: string[] | null   // ["07:00","12:00"] when schedule_kind === 'daily'; null/[] otherwise
  schedule_catch_up: boolean        // daily only: run a missed time later the same day
  owner: string
  status: 'active' | 'archived' | 'paused'
  critic_agent_id: string | null
  critic_mode: 'none' | 'builtin' | string  // "none" | "builtin" | "agent:<id>"
  monitor_model: string             // if set, overrides the agent's model for monitor runs
  budget_usd: number                // 0 = no limit
  budget_period: 'day' | 'week' | 'month' | 'total'
  context_summarisation: boolean    // if true, long follow-up chains are summarised before injection
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
  priority: number       // higher = runs first; default 0 = FIFO
  depends_on: string[]   // task IDs that must complete before this runs; [] = no deps
  created_at: string
  started_at: string | null
  completed_at: string | null
}

export interface CostSummary {
  id: string
  name: string
  total_cost_usd: number
  task_count: number
  tokens_in: number
  tokens_out: number
}

export interface UsageSummary {
  label: string
  total_cost_usd: number
  task_count: number
  tokens_in: number
  tokens_out: number
}

export interface DailyCost {
  date: string
  cost_usd: number
  tokens_in: number
  tokens_out: number
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
  core_plugins_enabled: boolean
  community_plugins_enabled: boolean
  obsidian_enabled: boolean
  obsidian_root: string
  obsidian_auto_write: boolean
  theme: string
}

export interface ObsidianVault {
  id: string
  name: string
  path: string
  context: string
  enabled: boolean
  sort_order: number
  created_at: string
}

export interface ObsidianDiscoveredVault {
  name: string
  path: string
  configured: boolean
}

export interface ObsidianWriteResult {
  vault: string
  path: string
  filename: string
}

export interface PluginRecord {
  id: string
  name: string
  type: 'notifier' | 'theme' | 'memory'
  kind: string
  is_core: boolean
  enabled: boolean
  config: string
  created_at: string
  updated_at: string
}

export interface NotificationRule {
  id: string
  plugin_id: string
  event_type: string
  project_id: string | null
  enabled: boolean
  template: string | null
  created_at: string
}

export interface PluginSchemaField {
  type: string
  title: string
  description?: string
  default?: unknown
  enum?: string[]
  secret?: boolean
}

export interface PluginConfigSchema {
  type: string
  properties: Record<string, PluginSchemaField>
  required?: string[]
}

export interface PluginChat {
  id: number
  title: string
  first_name: string
  type: string
}

export interface ThemeResponse {
  id: string
  kind: string
  label: string
  description?: string
  preview: string[]
  vars?: Record<string, string>
  is_built_in: boolean
  plugin_id?: string
}

export interface CostsResponse {
  total_cost_usd: number
  total_tokens_in: number
  total_tokens_out: number
  total_tasks: number
  by_agent: CostSummary[]
  by_project: CostSummary[]
  by_provider: UsageSummary[]
  by_model: UsageSummary[]
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
  artifact_path: string // absolute path to a .md artifact file, if any
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

export interface ProjectSummary {
  tasks_by_status: Record<string, number> | null
  total_tasks: number
  total_cost_usd: number
  last_activity: string | null
}

export interface ProjectSpend {
  spent_usd: number
  budget_usd: number
  budget_period: string
  remaining_usd: number
}

export interface ProjectFileEntry {
  name: string
  rel_path: string
  size_bytes: number
  modified_at: string
  ext: string
  is_artifact: boolean
}

export interface ProjectFileContent {
  content: string
  ext: string
  truncated: boolean
}

export interface CostInsightsBreakdownRow {
  id: string
  name: string
  model?: string
  provider_name?: string
  provider_id?: string
  actual_cost_usd: number
  tokens_in: number
  tokens_out: number
  task_count: number
  cost_per_task: number
  projected_monthly_usd: number
}

export interface CostInsightsRecommendation {
  severity: 'warning' | 'info'
  kind: string
  title: string
  detail: string
  agent_id?: string
  provider_id?: string
}

export interface CostInsights {
  period: { from: string; to: string }
  summary: {
    total_actual_usd: number
    projected_monthly_usd: number
    task_count: number
  }
  by_agent: CostInsightsBreakdownRow[]
  by_provider: CostInsightsBreakdownRow[]
  by_project: CostInsightsBreakdownRow[]
  recommendations: CostInsightsRecommendation[]
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
    test: (id: string) => request<{ ok: boolean; message: string; latency_ms: number }>(`/providers/${id}/test`, { method: 'POST' }),
    health: (id: string) => request<{ status: string; latency_ms: number; error?: string; checked_at: string }>(`/providers/${id}/health`),
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
    clearMemory: (id: string) => request<void>(`/agents/${id}/memory`, { method: 'DELETE' }),
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
    pause: (id: string) => request<Project>(`/projects/${id}/pause`, { method: 'POST', body: '{}' }),
    listAgents: (id: string) => request<Agent[]>(`/projects/${id}/agents`),
    assignAgent: (id: string, agentId: string) => request<void>(`/projects/${id}/agents`, { method: 'POST', body: JSON.stringify({ agent_id: agentId }) }),
    removeAgent: (id: string, agentId: string) => request<void>(`/projects/${id}/agents/${agentId}`, { method: 'DELETE' }),
    assignTeam: (id: string, teamId: string) => request<{ assigned: number, total: number, team: string }>(`/projects/${id}/teams`, { method: 'POST', body: JSON.stringify({ team_id: teamId }) }),
    summaries: () => request<Record<string, ProjectSummary>>('/projects/summaries'),
    getSpend: (id: string) => request<ProjectSpend>(`/projects/${id}/spend`),
    listFiles: (id: string) => request<ProjectFileEntry[]>(`/projects/${id}/files`),
    getFileContent: (id: string, relPath: string) =>
      request<ProjectFileContent>(`/projects/${id}/files/${relPath}`),
    generateDescription: (name: string, hint?: string, providerId?: string) =>
      request<{ description: string }>('/projects/generate-description', {
        method: 'POST',
        body: JSON.stringify({ name, hint: hint ?? '', provider_id: providerId ?? '' }),
      }),
    history: (id: string) => request<Task[]>(`/projects/${id}/history`),
    suggest: (id: string) =>
      request<{ suggestions: { title: string; description: string }[] }>(
        `/projects/${id}/suggest`, { method: 'POST', body: '{}' }
      ),
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
    estimate: (req: { agent_id: string; title?: string; description?: string }) =>
      request<{
        supported: boolean
        prompt_tokens: number
        estimated_output_tokens: { low: number; high: number }
        estimated_cost_usd: { low: number; high: number }
        provider: { type: string; model: string }
      }>('/tasks/estimate', { method: 'POST', body: JSON.stringify(req) }),
    generateDescription: (title: string, hint?: string, providerId?: string) =>
      request<{ description: string }>('/tasks/generate-description', {
        method: 'POST',
        body: JSON.stringify({ title, hint: hint ?? '', provider_id: providerId ?? '' }),
      }),
    listByAgent: (agentId: string) => request<Task[]>(`/agents/${agentId}/tasks`),
    listRunning: () => request<Task[]>('/tasks/running'),
    bump: (id: string) => request<Task>(`/tasks/${id}/bump`, { method: 'POST', body: '{}' }),
    cancel: (id: string) => request<void>(`/tasks/${id}/cancel`, { method: 'POST', body: '{}' }),
    forceReset: (id: string) => request<Task>(`/tasks/${id}/force-reset`, { method: 'POST', body: '{}' }),
  },
  search: {
    all: (q: string) => request<{ tasks: Task[]; agents: Agent[]; projects: Project[] }>(`/search?q=${encodeURIComponent(q)}`),
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
    getFileContent: (path: string) =>
      request<{ content: string; truncated: boolean }>(`/memos/file-content?path=${encodeURIComponent(path)}`),
  },
  plugins: {
    list: (type?: string) => request<PluginRecord[]>(type ? `/plugins?type=${type}` : '/plugins'),
    get: (id: string) => request<PluginRecord>(`/plugins/${id}`),
    create: (p: Partial<PluginRecord>) => request<PluginRecord>('/plugins', { method: 'POST', body: JSON.stringify(p) }),
    update: (id: string, p: Partial<PluginRecord>) => request<PluginRecord>(`/plugins/${id}`, { method: 'PUT', body: JSON.stringify(p) }),
    delete: (id: string) => request<void>(`/plugins/${id}`, { method: 'DELETE' }),
    enable: (id: string) => request<PluginRecord>(`/plugins/${id}/enable`, { method: 'POST' }),
    disable: (id: string) => request<PluginRecord>(`/plugins/${id}/disable`, { method: 'POST' }),
    test: (id: string) => request<{ status: string; message: string }>(`/plugins/${id}/test`, { method: 'POST' }),
    schema: (id: string) => request<PluginConfigSchema>(`/plugins/${id}/schema`),
    discoverChats: (id: string, botToken?: string) =>
      request<PluginChat[]>(
        `/plugins/${id}/chats${botToken ? `?bot_token=${encodeURIComponent(botToken)}` : ''}`
      ),
    rules: {
      list: (pluginId: string) => request<NotificationRule[]>(`/plugins/${pluginId}/rules`),
      create: (pluginId: string, r: Partial<NotificationRule>) =>
        request<NotificationRule>(`/plugins/${pluginId}/rules`, { method: 'POST', body: JSON.stringify(r) }),
      update: (pluginId: string, ruleId: string, r: Partial<NotificationRule>) =>
        request<NotificationRule>(`/plugins/${pluginId}/rules/${ruleId}`, { method: 'PUT', body: JSON.stringify(r) }),
      delete: (pluginId: string, ruleId: string) =>
        request<void>(`/plugins/${pluginId}/rules/${ruleId}`, { method: 'DELETE' }),
    },
  },
  themes: {
    list: () => request<ThemeResponse[]>('/themes'),
  },
  stats: {
    costs: () => request<CostsResponse>('/stats/costs'),
    costInsights: (from: string, to: string) =>
      request<CostInsights>(`/stats/costs/insights?from=${from}&to=${to}`),
  },
  providers_pricing: {
    update: (id: string, inputPerMToken: number, outputPerMToken: number) =>
      request<{ status: string }>(`/providers/${id}/pricing`, {
        method: 'PUT',
        body: JSON.stringify({ input_per_mtoken: inputPerMToken, output_per_mtoken: outputPerMToken }),
      }),
  },
  fs: {
    stat: (path: string) =>
      request<{ exists: boolean; is_dir: boolean }>(`/fs/stat?path=${encodeURIComponent(path)}`),
    mkdir: (path: string) =>
      request<{ created: boolean }>('/fs/mkdir', { method: 'POST', body: JSON.stringify({ path }) }),
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
    reset: () => request<{ status: string }>('/admin/reset', {
      method: 'POST',
      headers: { 'X-Confirm-Reset': 'RESET' },
    }),
  },
  obsidian: {
    listVaults: () => request<ObsidianVault[]>('/obsidian/vaults'),
    getVault: (id: string) => request<ObsidianVault>(`/obsidian/vaults/${id}`),
    createVault: (v: Partial<ObsidianVault>) =>
      request<ObsidianVault>('/obsidian/vaults', { method: 'POST', body: JSON.stringify(v) }),
    updateVault: (id: string, v: Partial<ObsidianVault>) =>
      request<ObsidianVault>(`/obsidian/vaults/${id}`, { method: 'PUT', body: JSON.stringify(v) }),
    deleteVault: (id: string) =>
      request<{ status: string }>(`/obsidian/vaults/${id}`, { method: 'DELETE' }),
    discover: (root?: string) =>
      request<ObsidianDiscoveredVault[]>(`/obsidian/discover${root ? `?root=${encodeURIComponent(root)}` : ''}`),
    generateContext: (vaultName: string, providerId?: string) =>
      request<{ context: string }>('/obsidian/generate-context', {
        method: 'POST',
        body: JSON.stringify({ vault_name: vaultName, provider_id: providerId ?? '' }),
      }),
    writeTask: (taskId: string, vaultId?: string, providerId?: string) =>
      request<ObsidianWriteResult>(`/tasks/${taskId}/obsidian-write`, {
        method: 'POST',
        body: JSON.stringify({ vault_id: vaultId ?? '', provider_id: providerId ?? '' }),
      }),
  },
  taskTemplates: {
    list: (projectId?: string) =>
      request<TaskTemplate[]>(`/task-templates${projectId ? `?project_id=${projectId}` : ''}`),
    create: (data: Partial<TaskTemplate>) =>
      request<TaskTemplate>('/task-templates', { method: 'POST', body: JSON.stringify(data) }),
    delete: (id: string) =>
      request<void>(`/task-templates/${id}`, { method: 'DELETE' }),
  },
}
