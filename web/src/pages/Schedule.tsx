import { useState } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import DeleteIcon from '@mui/icons-material/DeleteOutlined';
import AddIcon from '@mui/icons-material/AddOutlined';
import { StatusChip } from '../design-system/react';
import type { WorkloadState } from '../design-system/react/PowerRing';
import { usePolicies, useOverrides, apiDelete, type PolicyResponse } from '../hooks/useApi';
import { useQueryClient } from '@tanstack/react-query';
import { ScheduleDrawer } from '../components/ScheduleDrawer';
import { useNotify } from '../components/Notifications';

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

function expiresCountdown(expiresAt: string): string {
  const diff = new Date(expiresAt).getTime() - Date.now();
  if (diff <= 0) return 'Expired';
  const hours = Math.floor(diff / 3600000);
  const mins = Math.floor((diff % 3600000) / 60000);
  if (hours > 24) return `${Math.floor(hours / 24)}d ${hours % 24}h`;
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

export function Schedule() {
  const { data: policiesData, isLoading: policiesLoading, error: policiesError } = usePolicies();
  const { data: overridesData } = useOverrides();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const queryClient = useQueryClient();
  const notify = useNotify();

  const handleDeletePolicy = async (p: PolicyResponse) => {
    if (!confirm(`Delete policy "${p.metadata.name}"?`)) return;
    try {
      await apiDelete(`/policies/${p.metadata.namespace}/${p.metadata.name}`);
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      notify(`Policy "${p.metadata.name}" deleted`);
    } catch (err) {
      notify((err as Error).message, 'error');
    }
  };

  const handleDeleteOverride = async (o: { metadata: { name: string; namespace: string } }) => {
    if (!confirm(`Delete override "${o.metadata.name}"?`)) return;
    try {
      await apiDelete(`/overrides/${o.metadata.namespace}/${o.metadata.name}`);
      queryClient.invalidateQueries({ queryKey: ['overrides'] });
      notify(`Override "${o.metadata.name}" deleted`);
    } catch (err) {
      notify((err as Error).message, 'error');
    }
  };

  if (policiesError) return <Alert severity="error">{(policiesError as Error).message}</Alert>;

  // Merge policies + active overrides into one list
  const activeOverrides = overridesData?.items?.filter(o => o.status?.phase !== 'Expired') ?? [];

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 4 }}>
        <Box>
          <Typography variant="h4">Schedules</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            Power policies and temporary overrides.
          </Typography>
        </Box>
        <Button variant="contained" startIcon={<AddIcon />} onClick={() => setDrawerOpen(true)}>
          New Schedule
        </Button>
      </Stack>

      {policiesLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>Type</TableCell>
                <TableCell>State</TableCell>
                <TableCell>Window</TableCell>
                <TableCell>Days</TableCell>
                <TableCell>Scope</TableCell>
                <TableCell align="right">Targets</TableCell>
                <TableCell align="right">Priority</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {/* Policies */}
              {policiesData?.items?.map((p) => {
                const state: WorkloadState = p.spec.schedule.desiredState === 'on' ? 'running' : 'asleep';
                const window = p.spec.schedule.windows?.[0];
                return (
                  <TableRow key={`policy-${p.metadata.namespace}/${p.metadata.name}`} hover>
                    <TableCell>
                      <Typography variant="subtitle2">{p.metadata.name}</Typography>
                      {p.spec.description && (
                        <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>{p.spec.description}</Typography>
                      )}
                    </TableCell>
                    <TableCell>
                      <Chip label="Recurring" size="small" variant="outlined" sx={{ height: 22, fontSize: 11 }} />
                    </TableCell>
                    <TableCell>
                      <StatusChip state={state} label={p.spec.schedule.desiredState} />
                    </TableCell>
                    <TableCell>
                      {window ? (
                        <Typography variant="code">{window.start} — {window.end}</Typography>
                      ) : (
                        <Typography variant="caption" color="text.secondary">Always</Typography>
                      )}
                    </TableCell>
                    <TableCell>
                      {window?.days?.map((d) => (
                        <Chip key={d} label={DAYS[d]} size="small" variant="outlined" sx={{ mr: 0.5, height: 20, fontSize: 10 }} />
                      )) || <Chip label="All" size="small" variant="outlined" sx={{ height: 20, fontSize: 10 }} />}
                    </TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                        {p.spec.scope.namespaces?.slice(0, 3).map((ns) => (
                          <Chip key={ns} label={ns} size="small" variant="outlined" sx={{ height: 20, fontSize: 10 }} />
                        ))}
                        {(p.spec.scope.namespaces?.length ?? 0) > 3 && (
                          <Chip label={`+${(p.spec.scope.namespaces?.length ?? 0) - 3}`} size="small" sx={{ height: 20, fontSize: 10 }} />
                        )}
                      </Stack>
                    </TableCell>
                    <TableCell align="right">
                      <Typography variant="code">{p.status?.affectedTargets ?? 0}</Typography>
                    </TableCell>
                    <TableCell align="right">
                      <Typography variant="code">{p.spec.priority}</Typography>
                    </TableCell>
                    <TableCell align="right" sx={{ width: 40 }}>
                      <Tooltip title="Delete">
                        <IconButton size="small" onClick={() => handleDeletePolicy(p)}>
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                );
              })}

              {/* Overrides (inline with Temporary badge) */}
              {activeOverrides.map((o) => {
                const state: WorkloadState = o.spec.state === 'on' ? 'running' : 'asleep';
                return (
                  <TableRow key={`override-${o.metadata.namespace}/${o.metadata.name}`} hover>
                    <TableCell>
                      <Typography variant="subtitle2">{o.metadata.name}</Typography>
                      <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>{o.spec.reason}</Typography>
                    </TableCell>
                    <TableCell>
                      <Chip label="Temporary" size="small" color="warning" sx={{ height: 22, fontSize: 11 }} />
                    </TableCell>
                    <TableCell>
                      <StatusChip state={state} label={o.spec.state} />
                    </TableCell>
                    <TableCell>
                      <Typography variant="code" color="warning.main">{expiresCountdown(o.spec.expiresAt)}</Typography>
                    </TableCell>
                    <TableCell>—</TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                        {o.spec.scope.namespaces?.map((ns) => (
                          <Chip key={ns} label={ns} size="small" variant="outlined" sx={{ height: 20, fontSize: 10 }} />
                        ))}
                      </Stack>
                    </TableCell>
                    <TableCell align="right">
                      <Typography variant="code">{o.status?.phase === 'Active' ? '—' : '—'}</Typography>
                    </TableCell>
                    <TableCell align="right">
                      <Typography variant="code">{o.spec.priority}</Typography>
                    </TableCell>
                    <TableCell align="right" sx={{ width: 40 }}>
                      <Tooltip title="Delete">
                        <IconButton size="small" onClick={() => handleDeleteOverride(o)}>
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                );
              })}

              {(!policiesData?.items?.length && !activeOverrides.length) && (
                <TableRow>
                  <TableCell colSpan={9} align="center">
                    <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>
                      No schedules defined. Create your first one.
                    </Typography>
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      <ScheduleDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} onSuccess={(msg) => notify(msg)} />
    </Box>
  );
}
