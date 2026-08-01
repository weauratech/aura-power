import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Spinner, Flex, Text } from '@chakra-ui/react';
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
import { useAuth } from './hooks/useAuth';

export function App() {
  const { isAuthenticated, isLoading, authEnabled, user, logout } = useAuth();

  if (isLoading) {
    return (
      <Flex minH="100vh" justify="center" align="center" bg="gray.50">
        <Spinner size="lg" color="blue.500" />
        <Text ml={3} color="gray.500">Loading...</Text>
      </Flex>
    );
  }

  // Auth enabled but not logged in
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
          <Route path="/savings" element={<Savings />} />
          <Route path="/blocked" element={<Blocked />} />
          <Route path="/pending" element={<PendingApprovals />} />
          <Route path="/users" element={<Users />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
