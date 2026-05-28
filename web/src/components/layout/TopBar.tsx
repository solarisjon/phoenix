import { Link } from 'react-router-dom'

export function TopBar({ inboxCount, title }: { inboxCount: number; title: string }) {
  return (
    <header className="h-12 flex-shrink-0 border-b border-slate-800 flex items-center px-6 gap-4 bg-slate-950/50 backdrop-blur-sm">
      <h1 className="text-sm font-medium text-white">{title}</h1>
      <div className="flex-1" />
      <Link
        to="/inbox"
        className="flex items-center gap-2 text-slate-400 hover:text-white transition-colors text-sm"
      >
        <span>⊡</span>
        {inboxCount > 0 && (
          <span className="bg-violet-600 text-white text-xs font-medium px-1.5 py-0.5 rounded-full">
            {inboxCount}
          </span>
        )}
      </Link>
    </header>
  )
}
