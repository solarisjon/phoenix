import { lazy, Suspense } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AppLayout } from '@/components/layout/AppLayout'
import { AuthProvider, useAuth } from '@/context/AuthContext'
import LoginPage from '@/pages/LoginPage'

const DashboardPage     = lazy(() => import('@/pages/DashboardPage').then(m => ({ default: m.DashboardPage })))
const InboxPage         = lazy(() => import('@/pages/InboxPage').then(m => ({ default: m.InboxPage })))
const ProjectsWorkspace = lazy(() => import('@/pages/ProjectsWorkspace').then(m => ({ default: m.ProjectsWorkspace })))
const MonitorsPage      = lazy(() => import('@/pages/MonitorsPage').then(m => ({ default: m.MonitorsPage })))
const MonitorDetailPage = lazy(() => import('@/pages/MonitorDetailPage').then(m => ({ default: m.MonitorDetailPage })))
const TasksPage         = lazy(() => import('@/pages/TasksPage').then(m => ({ default: m.TasksPage })))
const TeamsPage         = lazy(() => import('@/pages/TeamsPage').then(m => ({ default: m.TeamsPage })))
const TeamDetailPage    = lazy(() => import('@/pages/TeamDetailPage').then(m => ({ default: m.TeamDetailPage })))
const SettingsPage      = lazy(() => import('@/pages/SettingsPage').then(m => ({ default: m.SettingsPage })))
const HelpPage          = lazy(() => import('@/pages/HelpPage').then(m => ({ default: m.HelpPage })))
const FeedPage          = lazy(() => import('@/pages/FeedPage'))
const BriefingPage      = lazy(() => import('@/pages/BriefingPage').then(m => ({ default: m.BriefingPage })))
const AgentActivityPage = lazy(() => import('@/pages/AgentActivityPage').then(m => ({ default: m.AgentActivityPage })))
const PluginsPage       = lazy(() => import('@/pages/PluginsPage').then(m => ({ default: m.PluginsPage })))
const CostInsightsPage  = lazy(() => import('@/pages/CostInsightsPage'))

function AuthenticatedApp() {
  const { user, isLoading, logout } = useAuth()

  if (isLoading) {
    return (
      <div className="min-h-screen bg-slate-950 flex items-center justify-center">
        <div className="text-slate-500 text-sm">Loading…</div>
      </div>
    )
  }

  if (!user) {
    return <LoginPage onLogin={() => window.location.reload()} />
  }

  return (
    <BrowserRouter>
      <AppLayout onLogout={logout} userName={user.name}>
        <Suspense fallback={<div className="text-slate-500 text-sm p-6">Loading…</div>}>
          <Routes>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/inbox" element={<InboxPage />} />
            <Route path="/briefing" element={<BriefingPage />} />
            <Route path="/projects" element={<ProjectsWorkspace />} />
            <Route path="/projects/:id" element={<ProjectsWorkspace />} />
            <Route path="/monitors" element={<MonitorsPage />} />
            <Route path="/monitors/:id" element={<MonitorDetailPage />} />
            <Route path="/tasks" element={<TasksPage />} />
            <Route path="/teams" element={<TeamsPage />} />
            <Route path="/teams/:id" element={<TeamDetailPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="/plugins" element={<PluginsPage />} />
            <Route path="/cost-insights" element={<CostInsightsPage />} />
            <Route path="/help" element={<HelpPage />} />
            <Route path="/feed" element={<FeedPage />} />
            <Route path="/queue" element={<Navigate to="/" replace />} />
            {/* Legacy redirects so old bookmarks still work */}
            <Route path="/agents/:id/activity" element={<AgentActivityPage />} />
            <Route path="/agents" element={<Navigate to="/settings?tab=agents" replace />} />
            <Route path="/providers" element={<Navigate to="/settings?tab=providers" replace />} />
          </Routes>
        </Suspense>
      </AppLayout>
    </BrowserRouter>
  )
}

export default function App() {
  return (
    <AuthProvider>
      <AuthenticatedApp />
    </AuthProvider>
  )
}
