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
import { EmptyState } from '../components/EmptyState';
import { ConfirmDialog } from '../components/ConfirmDialog';

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
  const [editingPolicy, setEditingPolicy] = useState<PolicyResponse | null>(null);
  const queryClient = useQueryClient();
  const notify = useNotify();
  const [deleteTarget, setDeleteTarget] = useState<{ type: 'policy' | 'override'; name: string; namespace: string } | null>(null);

  const handleDeletePolicy = async (p: PolicyResponse) => {
    setDeleteTarget({ type: 'policy', name: p.metadata.name, namespace: p.metadata.namespace });
  };

  const handleDeleteOverride = async (o: { metadata: { name: string; namespace: string } }) => {
    setDeleteTarget({ type: 'override', name: o.metadata.name, namespace: o.metadata.namespace });
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    try {
      if (deleteTarget.type === 'policy') {
        await apiDelete(`/policies/${deleteTarget.namespace}/${deleteTarget.name}`);
        queryClient.invalidateQueries({ queryKey: ['policies'] });
      } else {
        await apiDelete(`/overrides/${deleteTarget.namespace}/${deleteTarget.name}`);
        queryClient.invalidateQueries({ queryKey: ['overrides'] });
      }
      notify(`${deleteTarget.type === 'policy' ? 'Policy' : 'Override'} "${deleteTarget.name}" deleted`);
    } catch (err) {
      notify((err as Error).message, 'error');
    }
    setDeleteTarget(null);
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
        <Button variant="contained" startIcon={<AddIcon />} onClick={() => { setEditingPolicy(null); setDrawerOpen(true); }}>
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
                  <TableRow key={`policy-${p.metadata.namespace}/${p.metadata.name}`} hover sx={{ cursor: 'pointer' }} onClick={() => { setEditingPolicy(p); setDrawerOpen(true); }}>
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
                    <EmptyState
                      title="No schedules yet"
                      description="Create your first power schedule to start governing workloads and saving costs."
                      actionLabel="New Schedule"
                      onAction={() => setDrawerOpen(true)}
                    />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      <ScheduleDrawer
        open={drawerOpen}
        onClose={() => { setDrawerOpen(false); setEditingPolicy(null); }}
        onSuccess={(msg) => notify(msg)}
        editPolicy={editingPolicy ? {
          name: editingPolicy.metadata.name,
          namespace: editingPolicy.metadata.namespace,
          spec: editingPolicy.spec,
        } : null}
      />

      {/* Namespace Defaults Info */}
      {policiesData?.items?.some(p => p.metadata.name.startsWith('ns-default-')) && (
        <Box sx={{ mt: 5 }}>
          <Typography variant="h6" sx={{ mb: 2 }}>Namespace Defaults</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            These policies are auto-generated from namespace annotations (<code>aura.sh/default-schedule</code>).
            Annotate a namespace to apply a built-in schedule:
          </Typography>
          <Box sx={{ p: 2, borderRadius: 1, border: 1, borderColor: 'divider', bgcolor: 'background.paper', mb: 2 }}>
            <Typography variant="code" sx={{ fontSize: 12, display: 'block' }}>
              kubectl annotate namespace dev aura.sh/default-schedule=business-hours
            </Typography>
          </Box>
          <Typography variant="caption" color="text.secondary">
            Available schedules: <strong>business-hours</strong> (Mon-Fri 08-18), <strong>always-off</strong>, <strong>weekdays-only</strong> (Mon-Fri full day)
          </Typography>
        </Box>
      )}

      <ConfirmDialog
        open={!!deleteTarget}
        title={`Delete ${deleteTarget?.type === 'policy' ? 'Policy' : 'Override'}`}
        message={`Are you sure you want to delete "${deleteTarget?.name}"? This will remove governance from all affected targets.`}
        confirmLabel="Delete"
        onConfirm={confirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </Box>
  );
}
