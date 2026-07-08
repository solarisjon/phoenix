import { useState } from 'react'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'

// ---- Feature reference data ----

interface FeatureSection {
  heading: string
  icon: string
  features: { title: string; body: string; tip?: string }[]
}

const FEATURES: FeatureSection[] = [
  {
    heading: 'Core concepts',
    icon: '◈',
    features: [
      {
        title: 'Providers',
        body: 'The AI backend that does the work. Supports OpenAI-compatible LLM APIs, local models via Ollama, and CLI coding agents — pi, claude (claudecode), opencode, and crush. Configure in Settings → Providers.',
      },
      {
        title: 'Agents',
        body: 'A reusable AI persona with a name, behaviour description, guardrails, and a provider. One agent can run tasks across many projects. Configure in Settings → Agents.',
        tip: 'The Behaviour field replaces the old Persona + Instructions split. Write it as a single description of who the agent is and how it works.',
      },
      {
        title: 'Projects',
        body: 'Human-driven workspaces. Create a project, assign agents, then run tasks inside it. Tasks thread together over time — follow-ups carry the previous output as context automatically.',
      },
      {
        title: 'Monitors',
        body: 'Autonomous projects that run on a schedule. Set an interval (every 5 min to every day), assign an agent, and Phoenix fires tasks automatically. Monitors appear in their own section in the sidebar.',
        tip: 'Use the Run Now button on a monitor to fire an immediate run without waiting for the next scheduled tick.',
      },
      {
        title: 'Tasks',
        body: 'A unit of work — a title, description, and assigned agent. Tasks stream output in real time. When done you can follow up, retry, or pin the output to Briefing.',
      },
      {
        title: 'Teams',
        body: 'Named groups of agents. Assign a whole team to a project at once. Teams can be exported as a JSON bundle and imported into another Phoenix instance — useful for sharing agent configurations.',
        tip: 'API keys are never included in exports. The importer is prompted to supply them.',
      },
    ],
  },
  {
    heading: 'Working with tasks',
    icon: '✦',
    features: [
      {
        title: 'Follow-ups',
        body: 'Reply to any completed task to continue the thread. The agent receives the previous output as context automatically — no copy-pasting required. Follow-ups appear indented under the original task card.',
      },
      {
        title: 'Quick Tasks (\u2318K)',
        body: 'Run a one-off task without opening a project. Click the ✦ button in the bottom-right corner or press ⌘K. Quick tasks run in a sandboxed project and appear in Tasks → All.',
      },
      {
        title: 'Retry',
        body: 'Re-run any failed or completed task with the same input. Useful when a model times out or returns empty output. The original task is preserved; retry creates a fresh run.',
      },
      {
        title: 'Cancel',
        body: 'Stop a running task immediately. The subprocess is killed, the task is marked failed, and the agent is freed for the next queued task.',
      },
      {
        title: 'Task queuing',
        body: 'If an agent has a Max Concurrent limit set, extra tasks queue automatically and start as soon as the agent has capacity. The Inbox badge counts running tasks separately.',
      },
      {
        title: 'Cost estimates',
        body: 'Before running a task, Phoenix shows an estimated cost based on prompt length and the provider’s pricing. Actual cost is shown on the card after completion. Cumulative costs are visible on the Dashboard.',
      },
    ],
  },
  {
    heading: 'Inbox',
    icon: '⊡',
    features: [
      {
        title: 'Awaiting approval',
        body: 'When an agent triggers a Hard Guardrail it pauses and waits. You can Approve (resume from where it stopped), Reject (mark failed), or Revise (send feedback and re-run with it).',
      },
      {
        title: 'Failed tasks',
        body: 'Tasks that errored appear in the inbox. Review the output for the error message, then retry or dismiss.',
      },
      {
        title: 'Agent hire proposals',
        body: 'Agents with the “can hire” flag enabled can propose new agents. The proposal appears in the inbox for review. Approve to create the agent (you pick the provider), or reject to discard.',
      },
      {
        title: 'Dismiss',
        body: 'Dismiss hides a task from the inbox without deleting it. The task and its output remain in the project thread. Use Dismiss All to clear all failed or awaiting tasks at once.',
      },
    ],
  },
  {
    heading: 'Briefing',
    icon: '📋',
    features: [
      {
        title: 'What is Briefing?',
        body: 'A dedicated space for important findings, summaries, and action items surfaced by your agents. Separate from the Inbox (which tracks task state) — Briefing is about content worth reading.',
      },
      {
        title: 'Agent-posted memos',
        body: 'Agents can embed memos directly in their output using a structured block. Phoenix extracts and saves them automatically when a task completes. The sidebar badge shows how many are unread.',
        tip: 'Tell your agent: include a MEMO_START block with a Title: line, optional Priority: high, your content, then MEMO_END. Multiple memos per task are supported.',
      },
      {
        title: 'Pin to Briefing',
        body: 'On any completed task card, click “📋 Pin to Briefing” to manually send the output as a memo. Useful when an agent didn’t post a memo but produced something worth keeping.',
      },
      {
        title: 'Read, Flag, Archive, Delete',
        body: 'Expanding a memo marks it read. Flag keeps it highlighted for follow-up. Archive hides it from the main view without deleting. Delete removes it permanently.',
      },
    ],
  },
  {
    heading: 'Organising projects',
    icon: '⊞',
    features: [
      {
        title: 'Tags',
        body: 'Add free-text tags to any project or monitor. Tags are lowercase and deduped automatically. Use them to group related work — e.g. “reporting”, “escalation”, “ai-research”.',
        tip: 'The tag input autocompletes from tags already used on other projects. Type and press Enter or comma to add.',
      },
      {
        title: 'Filter & Search',
        body: 'The Projects and Monitors pages both have a search bar and tag filter. Click a tag pill to filter to projects with that tag. Multiple tags act as AND — only projects with all selected tags are shown.',
      },
      {
        title: 'Sort & Group by tag',
        body: 'Sort by name, creation date, or choose “Group by tag” to organise the list into labelled sections. Untagged projects collect at the bottom.',
      },
      {
        title: 'Archive vs Delete',
        body: 'Archive hides a project from the active list but preserves everything — all tasks, outputs, and history. Restore it any time from Settings → Archived. Delete is permanent and removes all tasks too.',
        tip: 'Archive when a project is done but you might want to reference it later. Delete only when you’re certain you don’t need it.',
      },
    ],
  },
  {
    heading: 'Guardrails & safety',
    icon: '🔒',
    features: [
      {
        title: 'Soft guardrails',
        body: 'Advisory constraints on an agent. The agent tries to follow them but can deviate if needed — it will explain why in its output. Set per-agent in Settings → Agents.',
      },
      {
        title: 'Hard guardrails',
        body: 'Mandatory rules. If the agent’s task would violate one, it outputs GUARDRAIL_TRIGGERED: and pauses for human approval. You can approve, reject, or revise with feedback from the Inbox.',
      },
      {
        title: 'Global guardrails',
        body: 'Platform-wide rules that apply to every agent on every task. Set in Settings → System. When enabled, they are injected into every system prompt and override per-agent guardrails.',
        tip: 'Use global guardrails for non-negotiable platform policies — e.g. “never commit to git without approval”.',
      },
    ],
  },
  {
    heading: 'Agent capabilities',
    icon: '✶',
    features: [
      {
        title: 'Spawn agents',
        body: 'An agent with “Can spawn agents” enabled can create tasks for other existing agents via the Phoenix API. Useful for orchestration — a coordinator agent delegating subtasks to specialists.',
      },
      {
        title: 'Hire agents',
        body: 'An agent with “Can hire agents” enabled can propose entirely new agents. The proposal goes to the Inbox for human review. On approval, you choose the provider and the agent is created.',
      },
      {
        title: 'Critic reviews',
        body: 'Assign a critic agent to a project. After every completed task, the critic automatically reviews the output and posts a critique as a separate task card. Useful for quality control.',
      },
      {
        title: 'Model override',
        body: 'Each agent can override the provider’s default model. Useful when you want most agents on a fast/cheap model but specific agents on a more capable one.',
      },
    ],
  },
  {
    heading: 'Settings & system',
    icon: '⚙',
    features: [
      {
        title: 'Database backup & restore',
        body: 'Download a consistent SQLite snapshot at any time from Settings → System. The backup is WAL-consolidated and safe to take while tasks are running. Upload a backup to stage a restore — it applies on next restart.',
      },
      {
        title: 'Archived projects',
        body: 'Archived projects and monitors live in Settings → Archived. Restore them to the active list or permanently delete them from here.',
      },
      {
        title: 'Themes',
        body: 'Switch the UI colour theme from the picker at the bottom of the sidebar. Built-in themes (dark and light variants) are listed first; community themes load from enabled Theme plugins. Themes apply immediately and are stored in the DB.',
      },
      {
        title: 'Provider health checks',
        body: 'Phoenix pings each configured provider on a background interval. A coloured dot on the Providers page and in provider pickers shows status: 🟢 healthy, 🔴 unreachable, ◦ unknown. Use the Test button to trigger an immediate check.',
      },
    ],
  },
  {
    heading: 'Obsidian integration',
    icon: '💾',
    features: [
      {
        title: 'Vault configuration',
        body: 'Connect one or more Obsidian vaults in Settings → Obsidian. Each vault has a path and an optional context description injected into agent prompts so agents know what the vault contains.',
        tip: 'Use ✦ Generate with AI on the context field — Phoenix reads the vault directory and writes the description for you.',
      },
      {
        title: 'Auto-write after task completion',
        body: 'Enable “Auto-write to Obsidian” in Settings to have Phoenix generate and save a structured note for every completed task. Notes land in the configured vault automatically.',
      },
      {
        title: 'Agent-directed writes',
        body: 'Agents can embed ARTIFACT_START / Type: obsidian / ARTIFACT_END blocks in their output to explicitly write to a named vault. The runner extracts and saves the note on task completion.',
      },
      {
        title: 'Manual save',
        body: 'Click “Save to Obsidian” on any completed task card to trigger a one-off vault write without enabling auto-write globally.',
      },
    ],
  },
  {
    heading: 'Cost & performance',
    icon: '💰',
    features: [
      {
        title: 'Cost Insights',
        body: 'The Cost Insights page shows spend over time broken down by provider, agent, and project. Trend analysis flags anomalous spikes. Accessible from the sidebar.',
      },
      {
        title: 'Pre-run cost estimate',
        body: 'Before creating a task, click ≈ Estimate cost in the compose panel. Phoenix calculates prompt tokens and estimates output cost based on your provider’s configured pricing.',
      },
      {
        title: 'Max cost per run',
        body: 'Set a hard cost ceiling on an agent (Settings → Agents → Edit). When the prompt would exceed the budget, older context turns are dropped. If it still doesn’t fit, a configured fallback model is used instead.',
      },
      {
        title: 'Context summarisation',
        body: 'Enable on a project (Edit Project) to summarise older follow-up turns when a chain grows beyond ~8 000 chars. Typically reduces input token costs by 50–80% on long threads.',
      },
    ],
  },
  {
    heading: 'Dynamic orchestration',
    icon: '★',
    features: [
      {
        title: 'What is it?',
        body: 'When enabled (Settings → Orchestration), an orchestrator agent receives each new task, analyses it, selects the best model or agent, and optionally splits it into subtasks. Enable by toggling Dynamic Orchestration and selecting an orchestrator agent.',
      },
      {
        title: 'Model pools',
        body: 'Configure a pool of models in Settings → Orchestration. The orchestrator picks the most appropriate model for each task based on the configured selection strategy (e.g. cheapest, most capable, least busy).',
      },
      {
        title: 'Orchestration tasks',
        body: 'Tasks routed by the orchestrator show an ⚡ Orchestrator badge in the task detail view. Subtasks it creates show a ↓ Subtask badge.',
      },
    ],
  },
  {
    heading: 'Plugins',
    icon: '🧩',
    features: [
      {
        title: 'Notifiers',
        body: 'Send task events (completed, failed, awaiting approval, etc.) to external systems. Built-in: Telegram bot and generic Webhook (HMAC-SHA256 signed). Configure notification rules per plugin — choose which events trigger a notification.',
      },
      {
        title: 'Memory (Hindsight)',
        body: 'The Hindsight memory plugin stores observations from each completed task and injects recalled memories into future tasks for the same agent. Enable in Plugins → Memory. Clear an agent’s memory from the agent edit form.',
      },
      {
        title: 'Community themes',
        body: 'Install a Custom Theme plugin by pasting a CSS snippet. Phoenix scopes it to the --ph-* CSS variable set. Themes appear in the sidebar picker immediately after enabling.',
      },
    ],
  },
  {
    heading: 'Task templates & priority',
    icon: '⊡',
    features: [
      {
        title: 'Task templates',
        body: 'Save a task title + description as a reusable template from the compose panel. Templates can be global (available in all projects) or scoped to a specific project. Pick a template from the dropdown at the top of the compose form. Templates support {{date}} and {{project_name}} placeholders.',
      },
      {
        title: 'Task priority & bump',
        body: 'Tasks run in creation order by default. Click ⬆ Bump on any queued task to increment its priority score — higher priority tasks run first. Priority is shown as a P+N badge on task cards in the /tasks view.',
      },
      {
        title: 'Task dependency chains',
        body: 'In the compose panel, expand “Run after” to select tasks that must complete before this one is queued. The scheduler holds the task until all dependencies are in a completed state.',
      },
    ],
  },
]

// ---- Step definitions ----

interface Step {
  title: string
  icon: string
  concept: { label: string; text: string }
  instructions: { text: string }[]
  link: { label: string; href: string }
}

const STEPS: Step[] = [
  {
    title: 'Add a Provider',
    icon: '⚡',
    concept: {
      label: 'What is a Provider?',
      text: 'A Provider is the AI model or coding agent backend that does the actual thinking. Phoenix supports LLM APIs (OpenAI-compatible), local models via Ollama, and CLI-based coding agents like pi, claude, opencode, and crush.',
    },
    instructions: [
      { text: 'Go to Settings → Providers and click New Provider.' },
      { text: 'Choose a type: LLM for API-based models, or Coding Agent for CLI tools.' },
      { text: 'Fill in the connection details — API key and base URL for LLMs, binary path for CLI agents.' },
      { text: 'Save. Your provider is now available to assign to agents.' },
    ],
    link: { label: 'Go to Settings → Providers', href: '/settings?tab=providers' },
  },
  {
    title: 'Create an Agent',
    icon: '✦',
    concept: {
      label: 'What is an Agent?',
      text: 'An Agent is a reusable AI persona — not a conversation thread. It has a name, a persona description, instructions for how it should behave, and a provider that powers it. One agent can run many tasks across many projects over time.',
    },
    instructions: [
      { text: 'Go to Settings → Agents and click New Agent.' },
      { text: 'Give it a name and persona — e.g. "Senior Engineer, pragmatic, prefers concise answers".' },
      { text: 'Write instructions: what this agent does, what tools it can use, any rules it must follow.' },
      { text: 'Select the provider you added in Step 1 and save.' },
    ],
    link: { label: 'Go to Settings → Agents', href: '/settings?tab=agents' },
  },
  {
    title: 'Create a Project',
    icon: '⊞',
    concept: {
      label: 'What is a Project?',
      text: 'A Project is a workspace where you run tasks. It has a name, an optional working directory on disk, and one or more agents assigned to it. Tasks within a project can reference each other as follow-ups, building a thread of work over time.',
    },
    instructions: [
      { text: 'Go to Projects and click New Project.' },
      { text: 'Give it a name and optionally a working directory — the folder on disk the agent will work in.' },
      { text: 'Assign your agent to the project.' },
      { text: 'Save. You\'re ready to run tasks.' },
    ],
    link: { label: 'Go to Projects', href: '/projects' },
  },
  {
    title: 'Run your first Task',
    icon: '▶',
    concept: {
      label: 'What is a Task?',
      text: 'A Task is a unit of work you give to an agent. You write a title and description, pick an agent, and Phoenix runs it — streaming the output back in real time. Tasks can be followed up, retried, and threaded together.',
    },
    instructions: [
      { text: 'Open your project and click New Task.' },
      { text: 'Write a title and description. Be specific — the more context you give, the better the output.' },
      { text: 'Select an agent and click Run. Phoenix streams the output as the agent works.' },
      { text: 'When it\'s done, you can reply with a follow-up to continue the thread, or retry if something went wrong.' },
    ],
    link: { label: 'Go to Projects', href: '/projects' },
  },
]

// ---- Sub-components ----

function StepSidebar({ active, onSelect }: { active: number; onSelect: (i: number) => void }) {
  return (
    <nav className="w-48 flex-shrink-0 pt-1">
      {STEPS.map((step, i) => {
        const done = i < active
        const isActive = i === active
        return (
          <div key={i}>
            <button
              onClick={() => onSelect(i)}
              className={cn(
                'w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left transition-colors',
                isActive && 'bg-violet-600/20',
                !isActive && 'hover:bg-slate-800',
              )}
            >
              <span className={cn(
                'w-6 h-6 rounded-full border-2 flex items-center justify-center text-xs font-bold flex-shrink-0 transition-colors',
                done && 'bg-emerald-500 border-emerald-500 text-white',
                isActive && !done && 'bg-violet-600 border-violet-600 text-white',
                !done && !isActive && 'border-slate-700 text-slate-500',
              )}>
                {done ? '✓' : i + 1}
              </span>
              <span className={cn(
                'text-sm transition-colors',
                isActive && 'text-violet-300 font-medium',
                done && 'text-slate-500',
                !done && !isActive && 'text-slate-400',
              )}>
                {step.title}
              </span>
            </button>
            {i < STEPS.length - 1 && (
              <div className={cn(
                'w-0.5 h-3 ml-[22px] my-0.5 rounded-full transition-colors',
                i < active ? 'bg-emerald-600' : 'bg-slate-800',
              )} />
            )}
          </div>
        )
      })}
    </nav>
  )
}

function StepContent({ step, index, total, onPrev, onNext }: {
  step: Step
  index: number
  total: number
  onPrev: () => void
  onNext: () => void
}) {
  return (
    <div className="flex-1 bg-slate-900 border border-slate-800 rounded-xl p-7 min-h-0">
      {/* Badge */}
      <div className="inline-flex items-center gap-2 bg-violet-600/15 text-violet-400 text-xs font-semibold rounded-full px-3 py-1 mb-5">
        <span>{step.icon}</span>
        <span>Step {index + 1} of {total}</span>
      </div>

      <h2 className="text-xl font-bold text-white mb-2">{step.title}</h2>

      {/* Concept box */}
      <div className="bg-slate-800/60 border border-slate-700/60 rounded-lg px-4 py-3 mb-6">
        <p className="text-xs font-semibold text-violet-400 uppercase tracking-wider mb-1.5">{step.concept.label}</p>
        <p className="text-sm text-slate-300 leading-relaxed">{step.concept.text}</p>
      </div>

      {/* Instructions */}
      <ol className="space-y-3 mb-7">
        {step.instructions.map((ins, i) => (
          <li key={i} className="flex gap-3 items-start">
            <span className="w-5 h-5 rounded-full bg-slate-800 border border-slate-700 text-slate-500 text-xs font-bold flex items-center justify-center flex-shrink-0 mt-0.5">
              {i + 1}
            </span>
            <span className="text-sm text-slate-300 leading-relaxed">{ins.text}</span>
          </li>
        ))}
      </ol>

      {/* Actions */}
      <div className="flex items-center justify-between">
        <button
          onClick={onPrev}
          disabled={index === 0}
          className="text-sm text-slate-500 hover:text-slate-300 disabled:opacity-0 disabled:pointer-events-none transition-colors"
        >
          ← Back
        </button>

        <Link
          to={step.link.href}
          className="text-sm text-violet-400 hover:text-violet-300 underline underline-offset-2 transition-colors"
        >
          {step.link.label} →
        </Link>

        {index < total - 1 ? (
          <button
            onClick={onNext}
            className="bg-violet-600 hover:bg-violet-500 text-white text-sm font-semibold px-5 py-2 rounded-lg transition-colors"
          >
            Next →
          </button>
        ) : (
          <Link
            to="/"
            className="bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-semibold px-5 py-2 rounded-lg transition-colors"
          >
            Go to Dashboard →
          </Link>
        )}
      </div>
    </div>
  )
}

// ---- Page ----

export function HelpPage() {
  const [activeStep, setActiveStep] = useState(0)

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white">Help</h1>
        <p className="text-slate-400 text-sm mt-1">Getting started guide and full feature reference.</p>
      </div>

      <div className="flex gap-8 items-start">
        <StepSidebar active={activeStep} onSelect={setActiveStep} />
        <StepContent
          step={STEPS[activeStep]}
          index={activeStep}
          total={STEPS.length}
          onPrev={() => setActiveStep(i => Math.max(0, i - 1))}
          onNext={() => setActiveStep(i => Math.min(STEPS.length - 1, i + 1))}
        />
      </div>

      <hr className="border-slate-800" />

      {/* Feature reference */}
      <div className="space-y-10 pt-4">
        <div>
          <h2 className="text-xl font-bold text-white">Feature Reference</h2>
          <p className="text-slate-400 text-sm mt-1">Everything Phoenix can do, in one place.</p>
        </div>

        {FEATURES.map(section => (
          <div key={section.heading}>
            {/* Section heading */}
            <div className="flex items-center gap-2 mb-4">
              <span className="text-base">{section.icon}</span>
              <h3 className="text-sm font-semibold text-white uppercase tracking-wider">{section.heading}</h3>
              <div className="flex-1 h-px bg-slate-800 ml-2" />
            </div>

            {/* Feature cards */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {section.features.map(f => (
                <div key={f.title} className="bg-slate-900 border border-slate-800 rounded-xl p-4 space-y-1.5">
                  <p className="text-sm font-semibold text-violet-300">{f.title}</p>
                  <p className="text-sm text-slate-400 leading-relaxed">{f.body}</p>
                  {f.tip && (
                    <div className="flex gap-2 mt-2 bg-violet-950/30 border border-violet-800/30 rounded-lg px-3 py-2">
                      <span className="text-violet-400 text-xs shrink-0 mt-0.5">★</span>
                      <p className="text-xs text-violet-300/80 leading-relaxed">{f.tip}</p>
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
