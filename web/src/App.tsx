import { lazy, Suspense } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AppLayout } from '@/components/layout/AppLayout'

const DashboardPage     = lazy(() => import('@/pages/DashboardPage').then(m => ({ default: m.DashboardPage })))
const InboxPage         = lazy(() => import('@/pages/InboxPage').then(m => ({ default: m.InboxPage })))
const ProjectsPage      = lazy(() => import('@/pages/ProjectsPage').then(m => ({ default: m.ProjectsPage })))
const ProjectDetailPage = lazy(() => import('@/pages/ProjectDetailPage').then(m => ({ default: m.ProjectDetailPage })))
const MonitorsPage      = lazy(() => import('@/pages/MonitorsPage').then(m => ({ default: m.MonitorsPage })))
const MonitorDetailPage = lazy(() => import('@/pages/MonitorDetailPage').then(m => ({ default: m.MonitorDetailPage })))
const TasksPage         = lazy(() => import('@/pages/TasksPage').then(m => ({ default: m.TasksPage })))
const TeamsPage         = lazy(() => import('@/pages/TeamsPage').then(m => ({ default: m.TeamsPage })))
const TeamDetailPage    = lazy(() => import('@/pages/TeamDetailPage').then(m => ({ default: m.TeamDetailPage })))
const SettingsPage      = lazy(() => import('@/pages/SettingsPage').then(m => ({ default: m.SettingsPage })))
const HelpPage          = lazy(() => import('@/pages/HelpPage').then(m => ({ default: m.HelpPage })))
const AgentActivityPage = lazy(() => import('@/pages/AgentActivityPage').then(m => ({ default: m.AgentActivityPage })))

export default function App() {
  return (
    <BrowserRouter>
      <AppLayout>
        <Suspense fallback={<div className="text-slate-500 text-sm p-6">Loading…</div>}>
          <Routes>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/inbox" element={<InboxPage />} />
            <Route path="/projects" element={<ProjectsPage />} />
            <Route path="/projects/:id" element={<ProjectDetailPage />} />
            <Route path="/monitors" element={<MonitorsPage />} />
            <Route path="/monitors/:id" element={<MonitorDetailPage />} />
            <Route path="/tasks" element={<TasksPage />} />
            <Route path="/teams" element={<TeamsPage />} />
            <Route path="/teams/:id" element={<TeamDetailPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="/help" element={<HelpPage />} />
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
