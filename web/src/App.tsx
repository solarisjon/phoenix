import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AppLayout } from '@/components/layout/AppLayout'
import { DashboardPage } from '@/pages/DashboardPage'
import { InboxPage } from '@/pages/InboxPage'
import { ProjectsPage } from '@/pages/ProjectsPage'
import { ProjectDetailPage } from '@/pages/ProjectDetailPage'
import { AgentsPage } from '@/pages/AgentsPage'
import { ProvidersPage } from '@/pages/ProvidersPage'
import { TeamsPage } from '@/pages/TeamsPage'
import { TeamDetailPage } from '@/pages/TeamDetailPage'
import { TasksPage } from '@/pages/TasksPage'
import { SettingsPage } from '@/pages/SettingsPage'

export default function App() {
  return (
    <BrowserRouter>
      <AppLayout>
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/inbox" element={<InboxPage />} />
          <Route path="/projects" element={<ProjectsPage />} />
          <Route path="/projects/:id" element={<ProjectDetailPage />} />
          <Route path="/tasks" element={<TasksPage />} />
          <Route path="/teams" element={<TeamsPage />} />
          <Route path="/teams/:id" element={<TeamDetailPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          {/* Legacy redirects so old bookmarks still work */}
          <Route path="/agents" element={<Navigate to="/settings?tab=agents" replace />} />
          <Route path="/providers" element={<Navigate to="/settings?tab=providers" replace />} />
        </Routes>
      </AppLayout>
    </BrowserRouter>
  )
}
