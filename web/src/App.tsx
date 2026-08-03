import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import Typography from '@mui/material/Typography';
import { Layout } from './components/Layout';
import { Dashboard } from './pages/Dashboard';
import { Targets } from './pages/Targets';
import { NamespaceDetail } from './pages/NamespaceDetail';
import { TargetDetail } from './pages/TargetDetail';
import { Policies } from './pages/Policies';
import { RuleDetail } from './pages/RuleDetail';
import { Metrics } from './pages/Metrics';
import { Savings } from './pages/Savings';
import { Blocked } from './pages/Blocked';
import { Schedule } from './pages/Schedule';
import { Login } from './pages/Login';
import { PendingApprovals } from './pages/PendingApprovals';
import { Users } from './pages/Users';
import { Overrides } from './pages/Overrides';
import { AuditLog } from './pages/AuditLog';
import { Notifications } from './pages/Notifications';
import { useAuth } from './hooks/useAuth';

export function App() {
  const { isAuthenticated, isLoading, authEnabled, user, logout } = useAuth();

  if (isLoading) {
    return (
      <Box sx={{ minHeight: '100vh', display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 2 }}>
        <CircularProgress size={24} />
        <Typography color="text.secondary">Loading...</Typography>
      </Box>
    );
  }

  if (authEnabled && !isAuthenticated) {
    return <Login onLogin={() => window.location.reload()} />;
  }

  const handleLogout = async () => {
    await logout();
    window.location.reload();
  };

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout user={user} onLogout={handleLogout} />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/targets" element={<Targets />} />
          <Route path="/targets/:namespace" element={<NamespaceDetail />} />
          <Route path="/targets/:namespace/:name" element={<TargetDetail />} />
          <Route path="/rules" element={<Policies />} />
          <Route path="/rules/:name" element={<RuleDetail />} />
          <Route path="/schedule" element={<Schedule />} />
          <Route path="/metrics" element={<Metrics />} />
          <Route path="/policies" element={<Policies />} />
          <Route path="/overrides" element={<Overrides />} />
          <Route path="/audit" element={<AuditLog />} />
          <Route path="/notifications" element={<Notifications />} />
          <Route path="/savings" element={<Savings />} />
          <Route path="/blocked" element={<Blocked />} />
          <Route path="/pending" element={<PendingApprovals />} />
          <Route path="/users" element={<Users />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
