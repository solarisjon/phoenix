import { NavLink } from 'react-router-dom'
import { cn } from '@/lib/utils'

const nav = [
  { label: 'Dashboard', href: '/', icon: '◈' },
  { label: 'Inbox', href: '/inbox', icon: '⊡' },
  { label: 'Projects', href: '/projects', icon: '⊞' },
  { label: 'Agents', href: '/agents', icon: '⬡' },
  { label: 'Teams', href: '/teams', icon: '⬡⬡' },
  { label: 'Providers', href: '/providers', icon: '⊕' },
]

export function Sidebar({ inboxCount }: { inboxCount: number }) {
  return (
    <aside className="w-56 flex-shrink-0 bg-slate-900 border-r border-slate-800 flex flex-col">
      {/* Logo */}
      <div className="px-5 py-5 border-b border-slate-800">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-violet-600 flex items-center justify-center shadow-lg shadow-violet-900/50">
            <span className="text-white font-bold text-sm">✦</span>
          </div>
          <div>
            <p className="font-semibold text-white tracking-wide text-sm">Phoenix</p>
            <p className="text-xs text-slate-500">Agent Orchestrator</p>
          </div>
        </div>
      </div>

      {/* Nav */}
      <nav className="flex-1 px-3 py-3 space-y-0.5">
        {nav.map(({ label, href, icon }) => (
          <NavLink
            key={href}
            to={href}
            end={href === '/'}
            className={({ isActive }) =>
              cn(
                'flex items-center justify-between px-3 py-2 rounded-lg text-sm transition-colors group',
                isActive
                  ? 'bg-violet-600/20 text-violet-300'
                  : 'text-slate-400 hover:text-white hover:bg-slate-800'
              )
            }
          >
            <span className="flex items-center gap-2.5">
              <span className="text-base">{icon}</span>
              {label}
            </span>
            {label === 'Inbox' && inboxCount > 0 && (
              <span className="bg-violet-600 text-white text-xs font-medium px-1.5 py-0.5 rounded-full">
                {inboxCount}
              </span>
            )}
          </NavLink>
        ))}
      </nav>

      {/* Footer */}
      <div className="px-4 py-4 border-t border-slate-800">
        <p className="text-xs text-slate-600">Phoenix v0.1 · Phase 1</p>
      </div>
    </aside>
  )
}
