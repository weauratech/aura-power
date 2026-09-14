import { useContext } from 'react';
import { Link } from 'react-router-dom';
import Box from '@mui/material/Box';
import MuiLink from '@mui/material/Link';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';
import { CurrentUserContext } from '../contexts/CurrentUser';

const destinations = [
  ['/', 'Dashboard', 'Cluster status, governance coverage and recent activity.'],
  ['/targets', 'Targets', 'Discovered Kubernetes workloads and their power state.'],
  ['/schedule', 'Schedules', 'Recurring power policies and schedule overrides.'],
  ['/overrides', 'Overrides', 'Temporary exceptions to scheduled behavior.'],
  ['/savings', 'Savings', 'Estimated compute and cost savings.'],
  ['/blocked', 'Blocked targets', 'Workloads protected by a guardrail.'],
  ['/audit', 'Audit log', 'Power decisions and state transitions.'],
  ['/notifications', 'Notifications', 'Webhook destinations for power events.'],
  ['/cluster-metrics', 'Metrics', 'Cluster utilization and cost signals.'],
] as const;

export function SiteMap() {
  const user = useContext(CurrentUserContext);
  const roleDestinations = [
    ...(user?.role === 'approver' || user?.role === 'admin'
      ? [['/pending', 'Pending approvals', 'Operations waiting for approval.'] as const]
      : []),
    ...(user?.role === 'admin'
      ? [['/users', 'Users', 'Local users and application roles.'] as const]
      : []),
  ];

  return (
    <Box>
      <Typography component="h1" tabIndex={-1} variant="h4" sx={{ mb: 1 }}>All pages</Typography>
      <Typography color="text.secondary" sx={{ mb: 4 }}>
        Browse every area available to your current role.
      </Typography>
      <Stack component="ul" spacing={2} sx={{ p: 0, m: 0, listStyle: 'none' }}>
        {[...destinations, ...roleDestinations].map(([path, label, description]) => (
          <Box component="li" key={path}>
            <MuiLink component={Link} to={path} variant="h6">{label}</MuiLink>
            <Typography variant="body2" color="text.secondary">{description}</Typography>
          </Box>
        ))}
      </Stack>
    </Box>
  );
}
