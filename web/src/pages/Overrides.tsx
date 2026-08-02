import { useState } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import Button from '@mui/material/Button';
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
import { useOverrides, apiPost, apiDelete, type OverrideResponse } from '../hooks/useApi';
import { useQueryClient } from '@tanstack/react-query';

function expiresCountdown(expiresAt: string): { text: string; urgent: boolean } {
  const diff = new Date(expiresAt).getTime() - Date.now();
  if (diff <= 0) return { text: 'Expired', urgent: false };
  const hours = Math.floor(diff / 3600000);
  const mins = Math.floor((diff % 3600000) / 60000);
  if (hours > 24) return { text: `${Math.floor(hours / 24)}d ${hours % 24}h`, urgent: false };
  if (hours > 0) return { text: `${hours}h ${mins}m`, urgent: hours < 2 };
  return { text: `${mins}m`, urgent: true };
}

export function Overrides() {
  const { data, isLoading, error } = useOverrides();
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');

  // Form
  const [name, setName] = useState('');
  const [namespaces, setNamespaces] = useState('');
  const [workloadNames, setWorkloadNames] = useState('');
  const [state, setState] = useState('on');
  const [priority, setPriority] = useState('500');
  const [reason, setReason] = useState('');
  const [reference, setReference] = useState('');
  const [expiresIn, setExpiresIn] = useState('4'); // hours

  const handleCreate = async () => {
    setCreating(true);
    setCreateError('');
    try {
      const expiresAt = new Date(Date.now() + parseInt(expiresIn) * 3600000).toISOString();
      await apiPost('/overrides', {
        metadata: { name, namespace: 'aura-system' },
        spec: {
          scope: {
            namespaces: namespaces.split(',').map(s => s.trim()).filter(Boolean),
            workloadNames: workloadNames ? workloadNames.split(',').map(s => s.trim()).filter(Boolean) : undefined,
          },
          state,
          priority: parseInt(priority) || 500,
          expiresAt,
          reason,
          reference: reference || undefined,
        },
      });
      queryClient.invalidateQueries({ queryKey: ['overrides'] });
      setCreateOpen(false);
      resetForm();
    } catch (err) {
      setCreateError((err as Error).message);
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (o: OverrideResponse) => {
    if (!confirm(`Delete override "${o.metadata.name}"?`)) return;
    try {
      await apiDelete(`/overrides/${o.metadata.namespace}/${o.metadata.name}`);
      queryClient.invalidateQueries({ queryKey: ['overrides'] });
    } catch (err) {
      alert((err as Error).message);
    }
  };

  const resetForm = () => {
    setName(''); setNamespaces(''); setWorkloadNames(''); setState('on');
    setPriority('500'); setReason(''); setReference(''); setExpiresIn('4');
    setCreateError('');
  };

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  const active = data?.items?.filter(o => o.status?.phase !== 'Expired') ?? [];
  const expired = data?.items?.filter(o => o.status?.phase === 'Expired') ?? [];

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 4 }}>
        <Box>
          <Typography variant="h4">Overrides</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            Temporary exceptions to power policies.
          </Typography>
        </Box>
        <Button variant="contained" startIcon={<AddIcon />} onClick={() => setCreateOpen(true)}>
          New Override
        </Button>
      </Stack>

      {isLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : (
        <>
          {/* Active overrides */}
          <Typography variant="h6" sx={{ mb: 2 }}>Active ({active.length})</Typography>
          <TableContainer sx={{ mb: 4 }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Name</TableCell>
                  <TableCell>State</TableCell>
                  <TableCell>Priority</TableCell>
                  <TableCell>Expires In</TableCell>
                  <TableCell>Reason</TableCell>
                  <TableCell>Reference</TableCell>
                  <TableCell />
                </TableRow>
              </TableHead>
              <TableBody>
                {active.map((o) => {
                  const s: WorkloadState = o.spec.state === 'on' ? 'running' : 'asleep';
                  const countdown = expiresCountdown(o.spec.expiresAt);
                  return (
                    <TableRow key={`${o.metadata.namespace}/${o.metadata.name}`} hover>
                      <TableCell><Typography variant="subtitle2">{o.metadata.name}</Typography></TableCell>
                      <TableCell><StatusChip state={s} label={o.spec.state} /></TableCell>
                      <TableCell><Typography variant="code">{o.spec.priority}</Typography></TableCell>
                      <TableCell>
                        <Chip
                          label={countdown.text}
                          size="small"
                          color={countdown.urgent ? 'warning' : 'default'}
                          variant="outlined"
                          sx={{ fontFamily: "'Geist Mono', monospace", height: 22, fontSize: 11 }}
                        />
                      </TableCell>
                      <TableCell><Typography variant="body2" color="text.secondary">{o.spec.reason}</Typography></TableCell>
                      <TableCell>
                        {o.spec.reference && (
                          <Typography variant="code" color="text.secondary">{o.spec.reference}</Typography>
                        )}
                      </TableCell>
                      <TableCell align="right">
                        <Tooltip title="Delete">
                          <IconButton size="small" onClick={() => handleDelete(o)}>
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  );
                })}
                {active.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={7} align="center">
                      <Typography variant="body2" color="text.secondary" sx={{ py: 3 }}>No active overrides</Typography>
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </TableContainer>

          {/* Expired overrides */}
          {expired.length > 0 && (
            <>
              <Typography variant="h6" color="text.secondary" sx={{ mb: 2 }}>Expired ({expired.length})</Typography>
              <TableContainer>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>Name</TableCell>
                      <TableCell>State</TableCell>
                      <TableCell>Reason</TableCell>
                      <TableCell>Expired At</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {expired.map((o) => (
                      <TableRow key={`${o.metadata.namespace}/${o.metadata.name}`} sx={{ opacity: 0.6 }}>
                        <TableCell><Typography variant="body2">{o.metadata.name}</Typography></TableCell>
                        <TableCell><Typography variant="code">{o.spec.state}</Typography></TableCell>
                        <TableCell><Typography variant="body2" color="text.secondary">{o.spec.reason}</Typography></TableCell>
                        <TableCell><Typography variant="code" color="text.secondary">{new Date(o.spec.expiresAt).toLocaleString()}</Typography></TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </>
          )}
        </>
      )}

      {/* Create Override Dialog */}
      <Dialog open={createOpen} onClose={() => setCreateOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>New Override</DialogTitle>
        <DialogContent>
          {createError && <Alert severity="error" sx={{ mb: 2 }}>{createError}</Alert>}
          <Stack spacing={3} sx={{ mt: 1 }}>
            <TextField label="Override Name" value={name} onChange={e => setName(e.target.value)} fullWidth required />
            <TextField label="Namespaces" value={namespaces} onChange={e => setNamespaces(e.target.value)} fullWidth helperText="Comma-separated" required />
            <TextField label="Workload Names (optional)" value={workloadNames} onChange={e => setWorkloadNames(e.target.value)} fullWidth helperText="Comma-separated, leave empty for all in namespace" />
            <TextField label="Desired State" value={state} onChange={e => setState(e.target.value)} select fullWidth>
              <MenuItem value="on">On (keep running)</MenuItem>
              <MenuItem value="off">Off (power down)</MenuItem>
            </TextField>
            <TextField label="Priority" value={priority} onChange={e => setPriority(e.target.value)} type="number" helperText="Must be higher than conflicting policies" />
            <TextField label="Expires In (hours)" value={expiresIn} onChange={e => setExpiresIn(e.target.value)} type="number" helperText="Override auto-expires after this duration" />
            <TextField label="Reason" value={reason} onChange={e => setReason(e.target.value)} fullWidth required multiline rows={2} helperText="Min 3 characters — justification for the exception" />
            <TextField label="Reference (optional)" value={reference} onChange={e => setReference(e.target.value)} fullWidth helperText="Ticket or incident link (e.g. JIRA-1234)" />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreateOpen(false)}>Cancel</Button>
          <Button variant="contained" onClick={handleCreate} disabled={creating || !name || !namespaces || reason.length < 3}>
            {creating ? 'Creating...' : 'Create Override'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
