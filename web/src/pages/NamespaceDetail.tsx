import { useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Stack from '@mui/material/Stack';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import MuiLink from '@mui/material/Link';
import ScheduleIcon from '@mui/icons-material/ScheduleOutlined';
import { StatusChip } from '../design-system/react';
import type { WorkloadState } from '../design-system/react/PowerRing';
import { useTargets } from '../hooks/useApi';
import { ScheduleDrawer } from '../components/ScheduleDrawer';
import type { PowerTarget } from '../types';

function mapState(t: PowerTarget): WorkloadState {
  if (t.status.blocked) return 'failed';
  if (t.status.divergent) return 'scheduled';
  return t.status.observedState.powerState === 'on' ? 'running' : 'asleep';
}

function relativeTime(ts?: string): string {
  if (!ts) return '—';
  const diff = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function NamespaceDetail() {
  const { namespace } = useParams();
  const { data, isLoading, error } = useTargets();
  const [drawerOpen, setDrawerOpen] = useState(false);

  const targets = data?.targets?.filter(t => t.spec.targetRef.namespace === namespace) ?? [];

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 4 }}>
        <Box>
          <Typography variant="overline" color="text.secondary">Namespace</Typography>
          <Typography variant="h4">{namespace}</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            {targets.length} workloads
          </Typography>
        </Box>
        <Button variant="contained" startIcon={<ScheduleIcon />} onClick={() => setDrawerOpen(true)}>
          Schedule Namespace
        </Button>
      </Stack>

      {isLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>Kind</TableCell>
                <TableCell>State</TableCell>
                <TableCell>Desired</TableCell>
                <TableCell>Last Transition</TableCell>
                <TableCell align="right">Replicas</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {targets.map((t) => (
                <TableRow key={t.spec.targetRef.name} hover>
                  <TableCell>
                    <MuiLink component={Link} to={`/targets/${namespace}/${t.spec.targetRef.name}`} underline="hover">
                      {t.spec.targetRef.name}
                    </MuiLink>
                  </TableCell>
                  <TableCell><Typography variant="code" color="text.secondary">{t.spec.targetRef.kind}</Typography></TableCell>
                  <TableCell><StatusChip state={mapState(t)} /></TableCell>
                  <TableCell><Typography variant="code">{t.status.desiredState || '—'}</Typography></TableCell>
                  <TableCell><Typography variant="caption" color="text.secondary">{relativeTime(t.status.lastTransition)}</Typography></TableCell>
                  <TableCell align="right"><Typography variant="code">{t.status.observedState.replicas}</Typography></TableCell>
                </TableRow>
              ))}
              {targets.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} align="center">
                    <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>No targets in this namespace</Typography>
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      <ScheduleDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} prefill={{ namespaces: [namespace ?? ''] }} />
    </Box>
  );
}
