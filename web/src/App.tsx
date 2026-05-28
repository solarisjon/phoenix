import { BrowserRouter, Routes, Route } from 'react-router-dom'

function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="flex items-center justify-center h-full">
      <div className="text-center">
        <h2 className="text-2xl font-semibold text-white mb-2">{title}</h2>
        <p className="text-slate-400">Coming soon</p>
      </div>
    </div>
  )
}

function AppShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen bg-slate-950 text-slate-100 overflow-hidden">
      {/* Sidebar */}
      <aside className="w-60 flex-shrink-0 bg-slate-900 border-r border-slate-800 flex flex-col">
        {/* Logo */}
        <div className="px-6 py-5 border-b border-slate-800">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-lg bg-violet-600 flex items-center justify-center">
              <span className="text-white font-bold text-sm">P</span>
            </div>
            <span className="font-semibold text-white tracking-wide">Phoenix</span>
          </div>
        </div>

        {/* Nav */}
        <nav className="flex-1 px-3 py-4 space-y-1">
          {[
            { label: 'Dashboard', href: '/' },
            { label: 'Inbox', href: '/inbox', badge: 0 },
            { label: 'Projects', href: '/projects' },
            { label: 'Agents', href: '/agents' },
            { label: 'Providers', href: '/providers' },
          ].map(({ label, href, badge }) => (
            <a
              key={href}
              href={href}
              className="flex items-center justify-between px-3 py-2 rounded-md text-sm text-slate-400 hover:text-white hover:bg-slate-800 transition-colors"
            >
              <span>{label}</span>
              {badge !== undefined && badge > 0 && (
                <span className="bg-violet-600 text-white text-xs font-medium px-1.5 py-0.5 rounded-full">
                  {badge}
                </span>
              )}
            </a>
          ))}
        </nav>

        {/* Footer */}
        <div className="px-4 py-4 border-t border-slate-800">
          <p className="text-xs text-slate-600">v0.1.0 — Phase 1</p>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 flex flex-col overflow-hidden">
        {/* Top bar */}
        <header className="h-14 flex-shrink-0 border-b border-slate-800 flex items-center px-6 gap-4">
          <div className="flex-1" />
          <div className="flex items-center gap-2 text-slate-400 hover:text-white cursor-pointer transition-colors">
            <span className="text-sm">Inbox</span>
            <span className="bg-slate-800 text-slate-300 text-xs px-2 py-0.5 rounded-full">0</span>
          </div>
        </header>

        {/* Page content */}
        <div className="flex-1 overflow-auto p-6">
          {children}
        </div>
      </main>
    </div>
  )
}

function Dashboard() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <p className="text-slate-400 mt-1">Your agent orchestration control center</p>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-4 gap-4">
        {[
          { label: 'Active Projects', value: '0' },
          { label: 'Agents', value: '0' },
          { label: 'Tasks Running', value: '0' },
          { label: 'Total Cost', value: '$0.00' },
        ].map(({ label, value }) => (
          <div key={label} className="bg-slate-900 border border-slate-800 rounded-xl p-4">
            <p className="text-slate-400 text-sm">{label}</p>
            <p className="text-2xl font-bold text-white mt-1">{value}</p>
          </div>
        ))}
      </div>

      {/* Empty state */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl p-12 text-center">
        <div className="w-12 h-12 rounded-xl bg-violet-600/20 flex items-center justify-center mx-auto mb-4">
          <span className="text-violet-400 text-2xl">✦</span>
        </div>
        <h3 className="text-white font-medium mb-2">Ready to orchestrate</h3>
        <p className="text-slate-400 text-sm max-w-sm mx-auto">
          Start by configuring a provider, then create your first agent and project.
        </p>
        <div className="flex gap-3 justify-center mt-6">
          <a
            href="/providers"
            className="bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors"
          >
            Add Provider
          </a>
          <a
            href="/agents"
            className="bg-slate-800 hover:bg-slate-700 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors"
          >
            Create Agent
          </a>
        </div>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <AppShell>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/inbox" element={<PlaceholderPage title="Inbox" />} />
          <Route path="/projects" element={<PlaceholderPage title="Projects" />} />
          <Route path="/agents" element={<PlaceholderPage title="Agents" />} />
          <Route path="/providers" element={<PlaceholderPage title="Providers" />} />
        </Routes>
      </AppShell>
    </BrowserRouter>
  )
}
