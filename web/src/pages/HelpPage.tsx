import { useState } from 'react'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'

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
        <h1 className="text-2xl font-bold text-white">Getting Started</h1>
        <p className="text-slate-400 text-sm mt-1">Follow these four steps to set up Phoenix and run your first AI task.</p>
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

      {/* Concepts reference */}
      <div>
        <h2 className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3">Quick Reference</h2>
        <div className="grid grid-cols-2 gap-3">
          {[
            { term: 'Provider', def: 'The AI model or CLI agent backend (OpenAI, Ollama, pi, claude…).' },
            { term: 'Agent', def: 'A reusable AI persona with a name, instructions, and a provider.' },
            { term: 'Project', def: 'A workspace with a working directory and assigned agents.' },
            { term: 'Task', def: 'A unit of work. Runs inside a project, streams output live.' },
            { term: 'Monitor', def: 'An autonomous project driven by a heartbeat agent on a schedule.' },
            { term: 'Inbox', def: 'Tasks awaiting your approval or that failed and need attention.' },
            { term: 'Team', def: 'A named group of agents that can be exported and imported as a bundle.' },
            { term: 'Follow-up', def: 'A reply task that gets the previous task\'s output as context.' },
          ].map(({ term, def }) => (
            <div key={term} className="flex gap-3 bg-slate-900 border border-slate-800 rounded-lg px-4 py-3">
              <span className="text-sm font-semibold text-violet-300 w-20 flex-shrink-0">{term}</span>
              <span className="text-sm text-slate-400">{def}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
