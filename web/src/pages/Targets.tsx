import { useState, useMemo } from 'react';
import { Link } from 'react-router-dom';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import InputAdornment from '@mui/material/InputAdornment';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import MuiLink from '@mui/material/Link';
import Stack from '@mui/material/Stack';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import SearchIcon from '@mui/icons-material/SearchOutlined';
import ScheduleIcon from '@mui/icons-material/ScheduleOutlined';
import { StatusChip } from '../design-system/react';
import type { WorkloadState } from '../design-system/react/PowerRing';
import { useTargets } from '../hooks/useApi';
import { ScheduleDrawer } from '../components/ScheduleDrawer';
import { useNotify } from '../components/Notifications';
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

type StateFilter = 'all' | 'running' | 'asleep' | 'failed';

export function Targets() {
  const { data, isLoading, error } = useTargets();
  const notify = useNotify();
  const [search, setSearch] = useState('');
  const [stateFilter, setStateFilter] = useState<StateFilter>('all');
  const [groupByNs, setGroupByNs] = useState(false);

  // Schedule drawer
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerPrefill, setDrawerPrefill] = useState<{ namespaces?: string[]; workloadNames?: string[] }>({});

  const filtered = useMemo(() => {
    if (!data?.targets) return [];
    let items = data.targets;
    if (search) {
      const q = search.toLowerCase();
      items = items.filter(t =>
        t.spec.targetRef.name.toLowerCase().includes(q) ||
        t.spec.targetRef.namespace.toLowerCase().includes(q)
      );
    }
    if (stateFilter !== 'all') {
      items = items.filter(t => {
        const s = mapState(t);
        if (stateFilter === 'running') return s === 'running';
        if (stateFilter === 'asleep') return s === 'asleep';
        if (stateFilter === 'failed') return s === 'failed' || s === 'scheduled';
        return true;
      });
    }
    return items;
  }, [data, search, stateFilter]);

  const grouped = useMemo(() => {
    if (!groupByNs) return { '': filtered };
    const map: Record<string, PowerTarget[]> = {};
    filtered.forEach(t => {
      const ns = t.spec.targetRef.namespace;
      if (!map[ns]) map[ns] = [];
      map[ns].push(t);
    });
    return map;
  }, [filtered, groupByNs]);

  const openDrawerForNamespace = (ns: string) => {
    setDrawerPrefill({ namespaces: [ns] });
    setDrawerOpen(true);
  };

  const openDrawerForWorkload = (ns: string, name: string) => {
    setDrawerPrefill({ namespaces: [ns], workloadNames: [`${ns}/${name}`] });
    setDrawerOpen(true);
  };

  const openDrawerEmpty = () => {
    setDrawerPrefill({});
    setDrawerOpen(true);
  };

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 3 }}>
        <Box>
          <Typography variant="h4">Targets</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            {filtered.length} workloads {search || stateFilter !== 'all' ? '(filtered)' : ''}
          </Typography>
        </Box>
        <Button variant="contained" startIcon={<ScheduleIcon />} onClick={openDrawerEmpty}>
          Create Schedule
        </Button>
      </Stack>

      {/* Filters */}
      <Stack direction="row" spacing={2} alignItems="center" sx={{ mb: 3 }}>
        <TextField
          size="small"
          placeholder="Search by name or namespace..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          sx={{ width: 300 }}
          InputProps={{
            startAdornment: <InputAdornment position="start"><SearchIcon fontSize="small" /></InputAdornment>,
          }}
        />
        <ToggleButtonGroup
          size="small"
          value={stateFilter}
          exclusive
          onChange={(_, v) => v && setStateFilter(v)}
        >
          <ToggleButton value="all">All</ToggleButton>
          <ToggleButton value="running">Running</ToggleButton>
          <ToggleButton value="asleep">Asleep</ToggleButton>
          <ToggleButton value="failed">Issues</ToggleButton>
        </ToggleButtonGroup>
        <Chip
          label={groupByNs ? 'Grouped by NS' : 'Flat'}
          size="small"
          variant={groupByNs ? 'filled' : 'outlined'}
          onClick={() => setGroupByNs(!groupByNs)}
          sx={{ cursor: 'pointer' }}
        />
      </Stack>

      {isLoading ? (
        <Skeleton variant="rounded" height={400} />
      ) : (
        Object.entries(grouped).map(([ns, targets]) => (
          <Box key={ns} sx={{ mb: ns ? 4 : 0 }}>
            {ns && (
              <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 1, mt: 2 }}>
                <MuiLink
                  component={Link}
                  to={`/targets/${ns}`}
                  underline="hover"
                  sx={{ cursor: 'pointer' }}
                >
                  <Typography variant="overline" color="text.secondary">
                    {ns} ({targets.length})
                  </Typography>
                </MuiLink>
                <Button size="small" startIcon={<ScheduleIcon />} onClick={() => openDrawerForNamespace(ns)}>
                  Schedule
                </Button>
              </Stack>
            )}
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    {!ns && <TableCell>Namespace</TableCell>}
                    <TableCell>Name</TableCell>
                    <TableCell>Kind</TableCell>
                    <TableCell>State</TableCell>
                    <TableCell>Desired</TableCell>
                    <TableCell>Last Transition</TableCell>
                    <TableCell align="right">Replicas</TableCell>
                    <TableCell />
                  </TableRow>
                </TableHead>
                <TableBody>
                  {targets.map((t) => (
                    <TableRow key={`${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`} hover>
                      {!ns && (
                        <TableCell>
                          <MuiLink
                            component={Link}
                            to={`/targets/${t.spec.targetRef.namespace}`}
                            underline="hover"
                          >
                            <Typography variant="code" color="text.secondary">{t.spec.targetRef.namespace}</Typography>
                          </MuiLink>
                        </TableCell>
                      )}
                      <TableCell>
                        <MuiLink component={Link} to={`/targets/${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`} underline="hover">
                          {t.spec.targetRef.name}
                        </MuiLink>
                      </TableCell>
                      <TableCell><Typography variant="code" color="text.secondary">{t.spec.targetRef.kind}</Typography></TableCell>
                      <TableCell><StatusChip state={mapState(t)} /></TableCell>
                      <TableCell><Typography variant="code">{t.status.desiredState || '—'}</Typography></TableCell>
                      <TableCell><Typography variant="caption" color="text.secondary">{relativeTime(t.status.lastTransition)}</Typography></TableCell>
                      <TableCell align="right"><Typography variant="code">{t.status.observedState.replicas}</Typography></TableCell>
                      <TableCell align="right" sx={{ width: 40 }}>
                        <Button size="small" sx={{ minWidth: 0, px: 1 }} onClick={() => openDrawerForWorkload(t.spec.targetRef.namespace, t.spec.targetRef.name)}>
                          <ScheduleIcon fontSize="small" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                  {targets.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={8} align="center">
                        <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>No targets found</Typography>
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </TableContainer>
          </Box>
        ))
      )}

      <ScheduleDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} prefill={drawerPrefill} onSuccess={(msg) => notify(msg)} />
    </Box>
  );
}
