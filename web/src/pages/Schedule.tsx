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
import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import TextField from '@mui/material/TextField';
import MenuItem from '@mui/material/MenuItem';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import DeleteIcon from '@mui/icons-material/DeleteOutlined';
import AddIcon from '@mui/icons-material/AddOutlined';
import { StatusChip } from '../design-system/react';
import type { WorkloadState } from '../design-system/react/PowerRing';
import { usePolicies, apiPost, apiDelete, type PolicyResponse } from '../hooks/useApi';
import { useQueryClient } from '@tanstack/react-query';

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export function Schedule() {
  const { data, isLoading, error } = usePolicies();
  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');
  const queryClient = useQueryClient();

  // Form state
  const [name, setName] = useState('');
  const [namespaces, setNamespaces] = useState('');
  const [desiredState, setDesiredState] = useState('off');
  const [start, setStart] = useState('20:00');
  const [end, setEnd] = useState('08:00');
  const [timezone, setTimezone] = useState('America/Sao_Paulo');
  const [days, setDays] = useState<number[]>([1, 2, 3, 4, 5]);
  const [priority, setPriority] = useState('100');
  const [description, setDescription] = useState('');

  const handleCreate = async () => {
    setCreating(true);
    setCreateError('');
    try {
      await apiPost('/policies', {
        metadata: { name, namespace: 'aura-system' },
        spec: {
          scope: { namespaces: namespaces.split(',').map(s => s.trim()).filter(Boolean) },
          schedule: {
            desiredState,
            windows: [{ start, end, timezone, days: days.length > 0 ? days : undefined }],
          },
          priority: parseInt(priority) || 100,
          description,
        },
      });
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      setCreateOpen(false);
      resetForm();
    } catch (err) {
      setCreateError((err as Error).message);
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (p: PolicyResponse) => {
    if (!confirm(`Delete policy "${p.metadata.name}"?`)) return;
    try {
      await apiDelete(`/policies/${p.metadata.namespace}/${p.metadata.name}`);
      queryClient.invalidateQueries({ queryKey: ['policies'] });
    } catch (err) {
      alert((err as Error).message);
    }
  };

  const resetForm = () => {
    setName('');
    setNamespaces('');
    setDesiredState('off');
    setStart('20:00');
    setEnd('08:00');
    setTimezone('America/Sao_Paulo');
    setDays([1, 2, 3, 4, 5]);
    setPriority('100');
    setDescription('');
    setCreateError('');
  };

  const toggleDay = (d: number) => {
    setDays(prev => prev.includes(d) ? prev.filter(x => x !== d) : [...prev, d].sort());
  };

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 4 }}>
        <Box>
          <Typography variant="h4">Schedules</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            Power policies define when workloads are on or off.
          </Typography>
        </Box>
        <Button variant="contained" startIcon={<AddIcon />} onClick={() => setCreateOpen(true)}>
          New Policy
        </Button>
      </Stack>

      {isLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Policy</TableCell>
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
              {data?.items?.map((p) => {
                const state: WorkloadState = p.spec.schedule.desiredState === 'on' ? 'running' : 'asleep';
                const window = p.spec.schedule.windows?.[0];
                return (
                  <TableRow key={`${p.metadata.namespace}/${p.metadata.name}`} hover>
                    <TableCell>
                      <Typography variant="subtitle2">{p.metadata.name}</Typography>
                      {p.spec.description && (
                        <Typography variant="caption" color="text.secondary">{p.spec.description}</Typography>
                      )}
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
                        <Chip key={d} label={DAYS[d]} size="small" variant="outlined" sx={{ mr: 0.5, height: 22, fontSize: 11 }} />
                      )) || <Chip label="Every day" size="small" variant="outlined" sx={{ height: 22, fontSize: 11 }} />}
                    </TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                        {p.spec.scope.namespaces?.map((ns) => (
                          <Chip key={ns} label={ns} size="small" variant="outlined" sx={{ height: 22, fontSize: 11 }} />
                        ))}
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
                        <IconButton size="small" onClick={() => handleDelete(p)}>
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                );
              })}
              {(!data?.items || data.items.length === 0) && (
                <TableRow>
                  <TableCell colSpan={8} align="center">
                    <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>
                      No schedules defined. Create your first policy.
                    </Typography>
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      {/* Create Policy Dialog */}
      <Dialog open={createOpen} onClose={() => setCreateOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>New Power Policy</DialogTitle>
        <DialogContent>
          {createError && <Alert severity="error" sx={{ mb: 3 }}>{createError}</Alert>}
          <Stack spacing={3} sx={{ mt: 1 }}>
            <TextField label="Policy Name" value={name} onChange={e => setName(e.target.value)} fullWidth required />
            <TextField label="Namespaces" value={namespaces} onChange={e => setNamespaces(e.target.value)} fullWidth helperText="Comma-separated (e.g. staging, dev)" />
            <TextField label="Desired State" value={desiredState} onChange={e => setDesiredState(e.target.value)} select fullWidth>
              <MenuItem value="on">On (keep running during window)</MenuItem>
              <MenuItem value="off">Off (power down during window)</MenuItem>
            </TextField>
            <Stack direction="row" spacing={2}>
              <TextField label="Start" value={start} onChange={e => setStart(e.target.value)} helperText="HH:MM" />
              <TextField label="End" value={end} onChange={e => setEnd(e.target.value)} helperText="HH:MM" />
            </Stack>
            <TextField label="Timezone" value={timezone} onChange={e => setTimezone(e.target.value)} fullWidth />
            <Box>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>Active Days</Typography>
              <Stack direction="row" spacing={1}>
                {DAYS.map((label, i) => (
                  <Chip
                    key={i}
                    label={label}
                    size="small"
                    variant={days.includes(i) ? 'filled' : 'outlined'}
                    color={days.includes(i) ? 'primary' : 'default'}
                    onClick={() => toggleDay(i)}
                    sx={{ cursor: 'pointer' }}
                  />
                ))}
              </Stack>
            </Box>
            <TextField label="Priority" value={priority} onChange={e => setPriority(e.target.value)} type="number" helperText="Higher wins (0-1000)" />
            <TextField label="Description" value={description} onChange={e => setDescription(e.target.value)} fullWidth multiline rows={2} />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreateOpen(false)}>Cancel</Button>
          <Button variant="contained" onClick={handleCreate} disabled={creating || !name || !namespaces}>
            {creating ? 'Creating...' : 'Create Policy'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
