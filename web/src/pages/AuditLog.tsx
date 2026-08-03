import { useState } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Stack from '@mui/material/Stack';
import Button from '@mui/material/Button';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import Chip from '@mui/material/Chip';
import TextField from '@mui/material/TextField';
import InputAdornment from '@mui/material/InputAdornment';
import Divider from '@mui/material/Divider';
import SearchIcon from '@mui/icons-material/SearchOutlined';
import { useAuditEvents } from '../hooks/useApi';
import { EmptyState } from '../components/EmptyState';

function formatTime(ts: string): string {
  const d = new Date(ts);
  return d.toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function relativeTime(ts: string): string {
  const diff = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function actionColor(action: string): 'success' | 'error' | 'warning' | 'info' | 'default' {
  if (action.includes('Down') || action.includes('down')) return 'info';
  if (action.includes('Restore') || action.includes('restore')) return 'success';
  if (action.includes('Error') || action.includes('error')) return 'error';
  if (action.includes('Override') || action.includes('override')) return 'warning';
  return 'default';
}

function actionLabel(action: string): string {
  const map: Record<string, string> = {
    'workload_powered_down': 'Powered Down',
    'workload_restored': 'Restored',
    'execution_error': 'Error',
    'policy_created': 'Policy Created',
    'override_created': 'Override Created',
  };
  return map[action] || action.replace(/_/g, ' ');
}

export function AuditLog() {
  const { data, isLoading, error } = useAuditEvents();
  const [search, setSearch] = useState('');

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  const events = data?.events?.filter(e => {
    if (!search) return true;
    const q = search.toLowerCase();
    return (
      e.spec.target.name.toLowerCase().includes(q) ||
      e.spec.target.namespace.toLowerCase().includes(q) ||
      e.spec.action.toLowerCase().includes(q)
    );
  }) ?? [];

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 4 }}>
        <Box>
          <Typography variant="h4">Audit Log</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            Power actions and state transitions
          </Typography>
        </Box>
        <Stack direction="row" alignItems="center" spacing={2}>
          <Button variant="outlined" size="small" href="/api/v1/audit/export" download>
            Export CSV
          </Button>
          <Typography variant="caption" color="text.disabled">
            {data?.total ?? 0} total events
          </Typography>
        </Stack>
      </Stack>

      <TextField
        size="small"
        placeholder="Filter by target name, namespace, or action..."
        value={search}
        onChange={e => setSearch(e.target.value)}
        sx={{ mb: 3, width: 400 }}
        InputProps={{
          startAdornment: <InputAdornment position="start"><SearchIcon fontSize="small" /></InputAdornment>,
        }}
      />

      {isLoading ? (
        <Skeleton variant="rounded" height={400} />
      ) : events.length === 0 ? (
        <EmptyState
          title="No audit events"
          description="Events are recorded when workloads are powered down, restored, or when policies change. Create a schedule to start generating events."
        />
      ) : (
        <Card>
          <CardContent sx={{ p: 0, '&:last-child': { pb: 0 } }}>
            <Stack divider={<Divider />}>
              {events.map((ev, i) => (
                <Box key={i} sx={{ px: 3, py: 2 }}>
                  <Stack direction="row" alignItems="flex-start" spacing={2}>
                    {/* Timeline dot */}
                    <Box sx={{ pt: 0.5 }}>
                      <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: `${actionColor(ev.spec.action)}.main` }} />
                    </Box>

                    {/* Content */}
                    <Box sx={{ flex: 1, minWidth: 0 }}>
                      <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 0.5 }}>
                        <Chip
                          label={actionLabel(ev.spec.action)}
                          size="small"
                          color={actionColor(ev.spec.action)}
                          sx={{ height: 22, fontSize: 11 }}
                        />
                        <Typography variant="subtitle2">
                          {ev.spec.target.namespace}/{ev.spec.target.name}
                        </Typography>
                        <Chip label={ev.spec.target.kind} size="small" variant="outlined" sx={{ height: 20, fontSize: 10 }} />
                      </Stack>
                      <Typography variant="body2" color="text.secondary">
                        {ev.spec.reason}
                        {ev.spec.ruleName && <> — rule: <Typography component="span" variant="code">{ev.spec.ruleName}</Typography></>}
                      </Typography>
                    </Box>

                    {/* Timestamp */}
                    <Box sx={{ textAlign: 'right', flexShrink: 0 }}>
                      <Typography variant="caption" color="text.disabled">{relativeTime(ev.spec.timestamp)}</Typography>
                      <Typography variant="caption" color="text.disabled" sx={{ display: 'block' }}>{formatTime(ev.spec.timestamp)}</Typography>
                    </Box>
                  </Stack>
                </Box>
              ))}
            </Stack>
          </CardContent>
        </Card>
      )}
    </Box>
  );
}
