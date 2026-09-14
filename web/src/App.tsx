import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import Typography from '@mui/material/Typography';
import { lazy, Suspense } from 'react';
import { Layout } from './components/Layout';
import { Login } from './pages/Login';
import { useAuth } from './hooks/useAuth';
import { CurrentUserContext } from './contexts/CurrentUser';

const Dashboard = lazy(() => import('./pages/Dashboard').then(module => ({ default: module.Dashboard })));
const Targets = lazy(() => import('./pages/Targets').then(module => ({ default: module.Targets })));
const NamespaceDetail = lazy(() => import('./pages/NamespaceDetail').then(module => ({ default: module.NamespaceDetail })));
const TargetDetail = lazy(() => import('./pages/TargetDetail').then(module => ({ default: module.TargetDetail })));
const Policies = lazy(() => import('./pages/Policies').then(module => ({ default: module.Policies })));
const RuleDetail = lazy(() => import('./pages/RuleDetail').then(module => ({ default: module.RuleDetail })));
const Metrics = lazy(() => import('./pages/Metrics').then(module => ({ default: module.Metrics })));
const Savings = lazy(() => import('./pages/Savings').then(module => ({ default: module.Savings })));
const Blocked = lazy(() => import('./pages/Blocked').then(module => ({ default: module.Blocked })));
const Schedule = lazy(() => import('./pages/Schedule').then(module => ({ default: module.Schedule })));
const PendingApprovals = lazy(() => import('./pages/PendingApprovals').then(module => ({ default: module.PendingApprovals })));
const Users = lazy(() => import('./pages/Users').then(module => ({ default: module.Users })));
const Overrides = lazy(() => import('./pages/Overrides').then(module => ({ default: module.Overrides })));
const AuditLog = lazy(() => import('./pages/AuditLog').then(module => ({ default: module.AuditLog })));
const Notifications = lazy(() => import('./pages/Notifications').then(module => ({ default: module.Notifications })));

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

  const handlePasswordChanged = async () => {
    try {
      await logout();
    } finally {
      // The password change already revoked this session on the server. Always
      // return to authentication even when the best-effort logout request fails.
      window.location.reload();
    }
  };

  return (
	<CurrentUserContext.Provider value={user}>
	<BrowserRouter>
	  <Suspense fallback={<Box sx={{ p: 4 }}><CircularProgress size={24} aria-label="Loading page" /></Box>}>
      <Routes>
        <Route element={<Layout user={user} onLogout={handleLogout} onPasswordChanged={handlePasswordChanged} />}>
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
	  </Suspense>
	</BrowserRouter>
    </CurrentUserContext.Provider>
  );
}
