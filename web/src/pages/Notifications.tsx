import { useId, useState, type KeyboardEvent } from 'react';
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
import Drawer from '@mui/material/Drawer';
import TextField from '@mui/material/TextField';
import MenuItem from '@mui/material/MenuItem';
import Switch from '@mui/material/Switch';
import FormControlLabel from '@mui/material/FormControlLabel';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import DeleteIcon from '@mui/icons-material/DeleteOutlined';
import AddIcon from '@mui/icons-material/AddOutlined';
import CloseIcon from '@mui/icons-material/CloseOutlined';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { apiPost, apiPut, apiDelete } from '../hooks/useApi';
import { useNotify } from '../components/Notifications';
import { EmptyState } from '../components/EmptyState';
import { ConfirmDialog } from '../components/ConfirmDialog';

export interface NotificationChannel {
  metadata: { name: string; namespace: string };
  spec: { type: string; url?: string; urlFrom?: { name: string; key: string }; events: string[]; namespaceFilter: string[]; throttle: string; enabled: boolean; deliveryPolicy?: string; maxDeliveryAttempts?: number };
  status?: { totalSent?: number; totalErrors?: number; lastError?: string; lastAttempt?: { phase?: string } };
}

export function buildNotificationChannelSpec(input: {
  type: string; url: string; events: string[]; nsFilter: string; throttle: string;
  enabled: boolean; deliveryPolicy: string; maxDeliveryAttempts: number;
}, editingChannel: NotificationChannel | null) {
  return {
    type: input.type,
    url: input.url || undefined,
    events: input.events.length > 0 ? input.events : undefined,
    namespaceFilter: input.nsFilter ? input.nsFilter.split(',').map(s => s.trim()).filter(Boolean) : undefined,
    throttle: input.throttle || undefined,
    enabled: input.enabled,
    deliveryPolicy: input.deliveryPolicy,
    maxDeliveryAttempts: input.deliveryPolicy === 'at-least-once' ? input.maxDeliveryAttempts : 1,
    ...(editingChannel?.spec.urlFrom && !input.url ? { urlFrom: editingChannel.spec.urlFrom } : {}),
  };
}

function useNotificationChannels() {
  return useQuery<{ items: NotificationChannel[]; count: number }>({
    queryKey: ['notification-channels'],
    queryFn: async () => {
      const res = await fetch('/api/v1/notification-channels', { credentials: 'same-origin' });
      if (!res.ok) {
        let detail = '';
        try {
          const payload = await res.json();
          detail = typeof payload?.error === 'string' ? payload.error : '';
        } catch {
          // The status code still gives operators an actionable failure signal.
        }
        throw new Error(detail ? `Failed to load channels: ${detail}` : `Failed to load channels (HTTP ${res.status})`);
      }
      const data = await res.json();
      return { items: data.items || [], count: data.count || 0 };
    },
    refetchInterval: 15000,
    retry: 1,
  });
}

const EVENT_OPTIONS = [
  'workload.powered_down',
  'workload.restored',
  'execution.error',
  'override.created',
  'override.expired',
];

function activateRow(event: KeyboardEvent<HTMLTableRowElement>, action: () => void) {
  if (event.target !== event.currentTarget) return;
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    action();
  }
}

export function Notifications() {
  const drawerTitleId = useId();
  const { data, isLoading, error } = useNotificationChannels();
  const queryClient = useQueryClient();
  const notify = useNotify();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingChannel, setEditingChannel] = useState<NotificationChannel | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<{ name: string; namespace: string } | null>(null);

  // Form state
  const [name, setName] = useState('');
  const [type, setType] = useState('google-chat');
  const [url, setUrl] = useState('');
  const [events, setEvents] = useState<string[]>([]);
  const [nsFilter, setNsFilter] = useState('');
  const [throttle, setThrottle] = useState('5m');
  const [enabled, setEnabled] = useState(true);
  const [deliveryPolicy, setDeliveryPolicy] = useState('at-most-once');
  const [maxDeliveryAttempts, setMaxDeliveryAttempts] = useState(5);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');

  const handleCreate = async () => {
    setCreating(true);
    setCreateError('');
    try {
      const spec = buildNotificationChannelSpec({ type, url, events, nsFilter, throttle, enabled, deliveryPolicy, maxDeliveryAttempts }, editingChannel);

      if (editingChannel) {
        // Update existing
        await apiPut(`/notification-channels/${editingChannel.metadata.namespace}/${editingChannel.metadata.name}`, { spec });
        notify(`Channel "${editingChannel.metadata.name}" updated`);
      } else {
        // Create new
        await apiPost('/notification-channels', {
          metadata: { name },
          spec,
        });
        notify(`Channel "${name}" created`);
      }
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] });
      setDrawerOpen(false);
      setEditingChannel(null);
      resetForm();
    } catch (err) {
      setCreateError((err as Error).message);
    } finally {
      setCreating(false);
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    try {
      await apiDelete(`/notification-channels/${deleteTarget.namespace}/${deleteTarget.name}`);
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] });
      notify(`Channel "${deleteTarget.name}" deleted`);
    } catch (err) {
      notify((err as Error).message, 'error');
    }
    setDeleteTarget(null);
  };

  const resetForm = () => {
    setName(''); setType('google-chat'); setUrl(''); setEvents([]);
    setNsFilter(''); setThrottle('5m'); setEnabled(true);
    setDeliveryPolicy('at-most-once'); setMaxDeliveryAttempts(5); setCreateError('');
  };

  const openEdit = (ch: NotificationChannel) => {
    setEditingChannel(ch);
    setName(ch.metadata.name);
    setType(ch.spec.type);
    setUrl(ch.spec.url || '');
    setEvents(ch.spec.events || []);
    setNsFilter(ch.spec.namespaceFilter?.join(', ') || '');
    setThrottle(ch.spec.throttle || '5m');
    setEnabled(ch.spec.enabled);
    setDeliveryPolicy(ch.spec.deliveryPolicy || 'at-most-once');
    setMaxDeliveryAttempts(ch.spec.maxDeliveryAttempts || 5);
    setDrawerOpen(true);
  };

  const openCreate = () => {
    setEditingChannel(null);
    resetForm();
    setDrawerOpen(true);
  };

  const toggleEvent = (ev: string) => {
    setEvents(prev => prev.includes(ev) ? prev.filter(e => e !== ev) : [...prev, ev]);
  };

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 4 }}>
        <Box>
          <Typography variant="h4">Notifications</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            Webhook channels for power event alerts.
          </Typography>
        </Box>
        <Button aria-label="Create a new notification channel" variant="contained" startIcon={<AddIcon />} onClick={openCreate}>
          New Channel
        </Button>
      </Stack>

      {isLoading ? (
        <Box role="status" aria-label="Loading notification channels">
          <Skeleton variant="rounded" height={200} />
        </Box>
      ) : !data?.items?.length ? (
        <EmptyState
          title="No notification channels"
          description="Create a webhook channel to receive alerts when workloads are powered down or restored."
          actionLabel="New Channel"
          onAction={openCreate}
        />
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>Type</TableCell>
                <TableCell>Events</TableCell>
                <TableCell>Status</TableCell>
                <TableCell>Delivery</TableCell>
                <TableCell align="right">Sent</TableCell>
                <TableCell align="right">Errors</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {data.items?.map((ch) => (
                <TableRow
                  key={`${ch.metadata.namespace}/${ch.metadata.name}`}
                  hover
                  tabIndex={0}
                  aria-label={`Edit notification channel ${ch.metadata.name}`}
                  sx={{ cursor: 'pointer' }}
                  onClick={() => openEdit(ch)}
                  onKeyDown={(event) => activateRow(event, () => openEdit(ch))}
                >
                  <TableCell>
                    <Typography variant="subtitle2">{ch.metadata.name}</Typography>
                  </TableCell>
                  <TableCell>
                    <Chip label={ch.spec.type} size="small" variant="outlined" sx={{ height: 22, fontSize: 11 }} />
                  </TableCell>
                  <TableCell>
                    {ch.spec.events?.length ? (
                      <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                        {ch.spec.events?.map(ev => (
                          <Chip key={ev} label={ev.split('.')[1]} size="small" sx={{ height: 20, fontSize: 10 }} />
                        ))}
                      </Stack>
                    ) : (
                      <Typography variant="caption" color="text.secondary">All events</Typography>
                    )}
                  </TableCell>
                  <TableCell>
                    <Chip
                      label={ch.spec.enabled ? 'Active' : 'Disabled'}
                      size="small"
                      color={ch.spec.enabled ? 'success' : 'default'}
                      sx={{ height: 22, fontSize: 11 }}
                    />
                  </TableCell>
                  <TableCell>
                    <Typography variant="caption">
                      {ch.spec.deliveryPolicy === 'at-least-once' ? 'At least once' : 'At most once'}
                      {ch.status?.lastAttempt?.phase ? ` · ${ch.status.lastAttempt.phase}` : ''}
                    </Typography>
                  </TableCell>
                  <TableCell align="right">
                    <Typography variant="code">{ch.status?.totalSent ?? 0}</Typography>
                  </TableCell>
                  <TableCell align="right">
                    <Typography variant="code" color={ch.status?.totalErrors ? 'error.main' : undefined}>
                      {ch.status?.totalErrors ?? 0}
                    </Typography>
                  </TableCell>
                  <TableCell align="right">
                    <Tooltip title="Delete">
                      <IconButton
                        size="small"
                        onClick={(event) => {
                          event.stopPropagation();
                          setDeleteTarget({ name: ch.metadata.name, namespace: ch.metadata.namespace });
                        }}
                        aria-label={`Delete notification channel ${ch.metadata.name}`}
                      >
                        <DeleteIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      {/* Create Channel Drawer */}
      <Drawer
        anchor="right"
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        PaperProps={{ role: 'dialog', 'aria-modal': true, 'aria-labelledby': drawerTitleId }}
        sx={{ '& .MuiDrawer-paper': { width: 400, maxWidth: '100vw', p: 0 } }}
      >
        <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ px: 3, py: 2.5, borderBottom: 1, borderColor: 'divider' }}>
            <Typography id={drawerTitleId} variant="h5">{editingChannel ? 'Edit Channel' : 'New Channel'}</Typography>
            <IconButton onClick={() => setDrawerOpen(false)} size="small" aria-label="Close notification channel drawer" autoFocus><CloseIcon /></IconButton>
          </Stack>

          <Box sx={{ flex: 1, overflow: 'auto', px: 3, py: 3 }}>
            {createError && <Alert severity="error" sx={{ mb: 3 }}>{createError}</Alert>}
            <Stack spacing={3}>
              <TextField label="Name" value={name} onChange={e => setName(e.target.value)} size="small" fullWidth required />
              <TextField label="Provider Type" value={type} onChange={e => setType(e.target.value)} select size="small" fullWidth>
                <MenuItem value="google-chat">Google Chat</MenuItem>
                <MenuItem value="slack">Slack</MenuItem>
                <MenuItem value="generic">Generic Webhook</MenuItem>
              </TextField>
              <TextField label="Webhook URL" value={url} onChange={e => setUrl(e.target.value)} size="small" fullWidth required={!editingChannel?.spec.urlFrom} placeholder={editingChannel?.spec.urlFrom ? `Stored in Secret ${editingChannel.spec.urlFrom.name}/${editingChannel.spec.urlFrom.key}` : 'https://...'} helperText={editingChannel?.spec.urlFrom && !url ? 'The existing Secret reference will be preserved.' : undefined} />
              <Box>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Events (empty = all)</Typography>
                <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                  {EVENT_OPTIONS.map(ev => (
                    <Chip
                      key={ev}
                      label={ev.split('.')[1]}
                      size="small"
                      variant={events.includes(ev) ? 'filled' : 'outlined'}
                      color={events.includes(ev) ? 'primary' : 'default'}
                      onClick={() => toggleEvent(ev)}
                      sx={{ cursor: 'pointer' }}
                    />
                  ))}
                </Stack>
              </Box>
              <TextField label="Namespace Filter" value={nsFilter} onChange={e => setNsFilter(e.target.value)} size="small" fullWidth helperText="Comma-separated (empty = all namespaces)" />
              <TextField label="Throttle" value={throttle} onChange={e => setThrottle(e.target.value)} size="small" fullWidth helperText="Min interval between notifications (e.g. 5m, 1h)" />
              <TextField label="Delivery Policy" value={deliveryPolicy} onChange={e => setDeliveryPolicy(e.target.value)} select size="small" fullWidth helperText="At-most-once avoids duplicate sends after an unknown outcome. At-least-once retries with a stable idempotency key.">
                <MenuItem value="at-most-once">At most once</MenuItem>
                <MenuItem value="at-least-once">At least once</MenuItem>
              </TextField>
              {deliveryPolicy === 'at-least-once' && (
                <TextField label="Maximum Delivery Attempts" type="number" value={maxDeliveryAttempts} onChange={e => setMaxDeliveryAttempts(Math.min(20, Math.max(1, Number(e.target.value))))} size="small" fullWidth inputProps={{ min: 1, max: 20 }} helperText="Provider requests, from 1 to 20. Preflight failures use a separate retry budget." />
              )}
              <FormControlLabel control={<Switch checked={enabled} onChange={e => setEnabled(e.target.checked)} />} label="Enabled" />
            </Stack>
          </Box>

          <Stack direction="row" spacing={2} sx={{ px: 3, py: 2.5, borderTop: 1, borderColor: 'divider' }}>
            <Button onClick={() => setDrawerOpen(false)} sx={{ flex: 1 }}>Cancel</Button>
            <Button variant="contained" onClick={handleCreate} disabled={creating || !name || (!url && !editingChannel?.spec.urlFrom)} sx={{ flex: 1 }}>
              {creating ? 'Saving...' : editingChannel ? 'Save Changes' : 'Create Channel'}
            </Button>
          </Stack>
        </Box>
      </Drawer>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Notification Channel"
        message={`Are you sure you want to delete "${deleteTarget?.name}"? No more notifications will be sent through this channel.`}
        confirmLabel="Delete"
        onConfirm={confirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </Box>
  );
}
