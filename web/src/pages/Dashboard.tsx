import { useMemo } from 'react';
import { Link } from 'react-router-dom';
import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Grid from '@mui/material/Grid';
import Typography from '@mui/material/Typography';
import Stack from '@mui/material/Stack';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import LinearProgress from '@mui/material/LinearProgress';
import Divider from '@mui/material/Divider';
import { useTheme } from '@mui/material/styles';
import ScheduleIcon from '@mui/icons-material/ScheduleOutlined';
import { PieChart, Pie, Cell, ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip as RTooltip, CartesianGrid } from 'recharts';
import { PowerRing } from '../design-system/react';
import { useQuery } from '@tanstack/react-query';
import { useTargets } from '../hooks/useApi';

interface DashboardData {
  summary: {
    totalTargets: number;
    poweredOn: number;
    poweredOff: number;
    blocked: number;
    divergent: number;
    governed: number;
    activePolicies: number;
    activeOverrides: number;
  };
  efficiency: number;
  savings: { cpuHours: number; memoryGiBHours: number; estimatedCost: number };
  recentEvents: Array<{
    timestamp: string;
    action: string;
    target: { namespace: string; name: string; kind: string };
    result: string;
    reason: string;
    ruleName?: string;
  }>;
  nextTransitions: Array<{
    policy: string;
    state: string;
    time: string;
    namespace: string;
  }>;
}

function useDashboard() {
  return useQuery<DashboardData>({
    queryKey: ['dashboard'],
    queryFn: async () => {
      const res = await fetch('/api/v1/dashboard', { credentials: 'same-origin' });
      if (!res.ok) throw new Error('Failed to load dashboard');
      return res.json();
    },
    refetchInterval: 10000,
  });
}

function formatRelativeTime(ts: string): string {
  const diff = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function actionIcon(action: string): string {
  if (action.includes('down') || action.includes('Down')) return '⏸';
  if (action.includes('restore') || action.includes('Restore')) return '▶';
  if (action.includes('error') || action.includes('Error')) return '⚠';
  return '●';
}

export function Dashboard() {
  const { data, isLoading, error } = useDashboard();
  const { data: targetsData } = useTargets();
  const theme = useTheme();

  // Namespace breakdown
  const barData = useMemo(() => {
    if (!targetsData?.targets) return [];
    const nsMap: Record<string, { on: number; off: number }> = {};
    targetsData.targets.forEach((t) => {
      const ns = t.spec.targetRef.namespace;
      if (!nsMap[ns]) nsMap[ns] = { on: 0, off: 0 };
      if (t.status.observedState.powerState === 'on') nsMap[ns].on++;
      else nsMap[ns].off++;
    });
    return Object.entries(nsMap)
      .map(([ns, v]) => ({ namespace: ns, on: v.on, off: v.off }))
      .sort((a, b) => (b.on + b.off) - (a.on + a.off))
      .slice(0, 8);
  }, [targetsData]);

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  if (isLoading || !data) {
    return (
      <Grid container spacing={3}>
        {Array.from({ length: 8 }).map((_, i) => (
          <Grid item xs={12} sm={6} md={3} key={i}>
            <Skeleton variant="rounded" height={100} />
          </Grid>
        ))}
      </Grid>
    );
  }

  const { summary, efficiency, savings, recentEvents, nextTransitions } = data;
  const onRatio = summary.totalTargets > 0 ? summary.poweredOn / summary.totalTargets : 0;
  const isDiscoveryMode = summary.activePolicies === 0;

  const pieData = [
    { name: 'On', value: summary.poweredOn },
    { name: 'Off', value: summary.poweredOff },
    { name: 'Blocked', value: summary.blocked },
  ];
  const pieColors = [theme.palette.success.main, theme.palette.text.disabled, theme.palette.error.main];

  return (
    <Box>
      {/* Header */}
      <Stack direction="row" alignItems="center" spacing={3} sx={{ mb: 5 }}>
        <PowerRing value={onRatio} state="running" size={52} label={`${Math.round(onRatio * 100)}% powered on`} />
        <Box>
          <Typography variant="h4">Cluster Overview</Typography>
          <Typography variant="body2" color="text.secondary">
            {summary.totalTargets} targets · {summary.governed} governed · {summary.activePolicies} policies active
          </Typography>
        </Box>
      </Stack>

      {/* Discovery Mode Banner */}
      {isDiscoveryMode && (
        <Card sx={{ mb: 4, border: 1, borderColor: 'success.main', bgcolor: 'success.light' }}>
          <CardContent sx={{ py: 3 }}>
            <Stack direction="row" alignItems="center" justifyContent="space-between">
              <Box>
                <Typography variant="h5" sx={{ mb: 0.5 }}>Welcome to Aura Power</Typography>
                <Typography variant="body2" color="text.secondary">
                  {summary.totalTargets} workloads discovered across your cluster.
                  {summary.governed === 0 && ' Create your first schedule to start saving.'}
                </Typography>
                <Stack direction="row" spacing={2} sx={{ mt: 1.5 }}>
                  <Chip label={`${summary.totalTargets} workloads`} size="small" />
                  <Chip label={`${summary.blocked} blocked`} size="small" color={summary.blocked > 0 ? 'warning' : 'default'} />
                  <Chip label="0 policies" size="small" />
                </Stack>
              </Box>
              <Button variant="contained" component={Link} to="/schedule" startIcon={<ScheduleIcon />}>
                Create First Schedule
              </Button>
            </Stack>
          </CardContent>
        </Card>
      )}

      {/* Stats row — equal width cards */}
      <Grid container spacing={3} sx={{ mb: 3 }}>
        <Grid item xs={12} sm={6} md={2}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Coverage</Typography>
              <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace" }}>{Math.round(efficiency)}%</Typography>
              <LinearProgress variant="determinate" value={efficiency} sx={{ mt: 1.5, height: 4, borderRadius: 2 }} />
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={2}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Savings</Typography>
              <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace", color: 'success.main' }}>
                ${savings.estimatedCost.toFixed(2)}
              </Typography>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
                {savings.cpuHours.toFixed(0)} CPU-h
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Powered On</Typography>
              <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace" }}>{summary.poweredOn}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Powered Off</Typography>
              <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace" }}>{summary.poweredOff}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Blocked</Typography>
              <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace", color: summary.blocked > 0 ? 'error.main' : undefined }}>{summary.blocked}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Divergent</Typography>
              <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace", color: summary.divergent > 0 ? 'warning.main' : undefined }}>{summary.divergent}</Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Charts + Activity row */}
      <Grid container spacing={3}>
        {/* Pie chart */}
        <Grid item xs={12} md={3}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>State Distribution</Typography>
              <ResponsiveContainer width="100%" height={180}>
                <PieChart>
                  <Pie data={pieData} cx="50%" cy="50%" innerRadius={45} outerRadius={70} dataKey="value" strokeWidth={0}>
                    {pieData.map((_, i) => <Cell key={i} fill={pieColors[i]} />)}
                  </Pie>
                  <RTooltip contentStyle={{ backgroundColor: theme.palette.background.paper, border: `1px solid ${theme.palette.divider}`, borderRadius: 8, fontSize: 13 }} />
                </PieChart>
              </ResponsiveContainer>
              <Stack direction="row" spacing={2} justifyContent="center" sx={{ mt: 1 }}>
                {pieData.map((d, i) => (
                  <Stack key={d.name} direction="row" alignItems="center" spacing={0.5}>
                    <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: pieColors[i] }} />
                    <Typography variant="caption">{d.name}</Typography>
                  </Stack>
                ))}
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Bar chart */}
        <Grid item xs={12} md={5}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>Targets by Namespace</Typography>
              {barData.length > 0 ? (
                <ResponsiveContainer width="100%" height={200}>
                  <BarChart data={barData} layout="vertical" margin={{ left: 0, right: 16 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke={theme.palette.divider} horizontal={false} />
                    <XAxis type="number" tick={{ fontSize: 11, fill: theme.palette.text.secondary }} />
                    <YAxis type="category" dataKey="namespace" tick={{ fontSize: 11, fill: theme.palette.text.secondary }} width={110} />
                    <RTooltip contentStyle={{ backgroundColor: theme.palette.background.paper, border: `1px solid ${theme.palette.divider}`, borderRadius: 8, fontSize: 12 }} />
                    <Bar dataKey="on" name="On" fill={theme.palette.success.main} radius={[0, 3, 3, 0]} stackId="a" />
                    <Bar dataKey="off" name="Off" fill={theme.palette.text.disabled} radius={[0, 3, 3, 0]} stackId="a" />
                  </BarChart>
                </ResponsiveContainer>
              ) : (
                <Typography variant="body2" color="text.secondary">No data</Typography>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* Recent Activity */}
        <Grid item xs={12} md={4}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>Recent Activity</Typography>
              {recentEvents.length === 0 ? (
                <Typography variant="body2" color="text.secondary">No recent events</Typography>
              ) : (
                <Stack spacing={0} divider={<Divider />}>
                  {recentEvents.slice(0, 7).map((ev, i) => (
                    <Box key={i} sx={{ py: 1.5 }}>
                      <Stack direction="row" alignItems="flex-start" spacing={1.5}>
                        <Typography sx={{ fontSize: 14, lineHeight: 1.6 }}>{actionIcon(ev.action)}</Typography>
                        <Box sx={{ flex: 1, minWidth: 0 }}>
                          <Typography variant="body2" sx={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {ev.target.name}
                          </Typography>
                          <Typography variant="caption" color="text.secondary">
                            {ev.reason || ev.action}
                          </Typography>
                        </Box>
                        <Typography variant="caption" color="text.disabled" sx={{ whiteSpace: 'nowrap' }}>
                          {formatRelativeTime(ev.timestamp)}
                        </Typography>
                      </Stack>
                    </Box>
                  ))}
                </Stack>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* Next Transitions */}
        {nextTransitions.length > 0 && (
          <Grid item xs={12} md={4}>
            <Card>
              <CardContent>
                <Typography variant="h6" sx={{ mb: 2 }}>Upcoming Transitions</Typography>
                <Stack spacing={1.5}>
                  {nextTransitions.map((t, i) => (
                    <Stack key={i} direction="row" alignItems="center" justifyContent="space-between">
                      <Box>
                        <Typography variant="body2" sx={{ fontWeight: 500 }}>{t.policy}</Typography>
                        <Typography variant="caption" color="text.secondary">{t.namespace}</Typography>
                      </Box>
                      <Stack direction="row" alignItems="center" spacing={1}>
                        <Chip label={t.state} size="small" color={t.state === 'on' ? 'success' : 'default'} sx={{ height: 22, fontSize: 11 }} />
                        <Typography variant="code" color="text.secondary">{t.time}</Typography>
                      </Stack>
                    </Stack>
                  ))}
                </Stack>
              </CardContent>
            </Card>
          </Grid>
        )}
      </Grid>
    </Box>
  );
}
