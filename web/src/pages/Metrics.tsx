import { useState } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Grid from '@mui/material/Grid';
import Stack from '@mui/material/Stack';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import LinearProgress from '@mui/material/LinearProgress';
import { useTheme } from '@mui/material/styles';
import { XAxis, YAxis, CartesianGrid, Tooltip as RTooltip, ResponsiveContainer, Area, AreaChart } from 'recharts';
import { useClusterMetrics, useCostSummary } from '../hooks/useMetrics';
import { useProviderStatus } from '../hooks/useProviderStatus';

type TimeRange = '1h' | '6h' | '24h' | '7d';

function formatTimestamp(ts: string, range: TimeRange): string {
  const d = new Date(ts);
  if (range === '1h' || range === '6h') return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
  if (range === '24h') return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

function formatBytes(bytes: number): string {
  const gb = bytes / (1024 * 1024 * 1024);
  if (gb >= 1) return `${gb.toFixed(1)} GiB`;
  const mb = bytes / (1024 * 1024);
  return `${mb.toFixed(0)} MiB`;
}

function formatCores(cores: number): string {
  if (cores >= 1) return `${cores.toFixed(2)} cores`;
  return `${(cores * 1000).toFixed(0)}m`;
}

export function Metrics() {
  const [range, setRange] = useState<TimeRange>('24h');
  const { metricsAvailable, isLoading: providerLoading } = useProviderStatus();
  const { data: clusterData, isLoading: metricsLoading, error: metricsError } = useClusterMetrics(range);
  const { data: costData } = useCostSummary();
  const theme = useTheme();

  if (providerLoading) return <Skeleton variant="rounded" height={400} />;

  if (!metricsAvailable) {
    return (
      <Box>
        <Typography variant="h4" sx={{ mb: 4 }}>Metrics</Typography>
        <Alert severity="info">
          Metrics provider not available. Ensure <code>server.prometheus.url</code> is configured in your Helm values and Prometheus is reachable from the server pod.
        </Alert>
      </Box>
    );
  }

  // Compute current utilization from latest samples
  const latestCPU = clusterData?.cpuUsage?.slice(-1)[0]?.value ?? 0;
  const latestCPUCap = clusterData?.cpuCapacity?.slice(-1)[0]?.value ?? 1;
  const latestMem = clusterData?.memoryUsage?.slice(-1)[0]?.value ?? 0;
  const latestMemCap = clusterData?.memoryCapacity?.slice(-1)[0]?.value ?? 1;
  const cpuPct = (latestCPU / latestCPUCap) * 100;
  const memPct = (latestMem / latestMemCap) * 100;
  const nodeCount = clusterData?.nodeCount?.slice(-1)[0]?.value ?? 0;

  // Prepare time-series data for charts
  const cpuChartData = clusterData?.cpuUsage?.map((s, i) => ({
    time: s.timestamp,
    usage: s.value,
    capacity: clusterData.cpuCapacity?.[i]?.value ?? 0,
    requested: clusterData.cpuRequested?.[i]?.value ?? 0,
  })) ?? [];

  const memChartData = clusterData?.memoryUsage?.map((s, i) => ({
    time: s.timestamp,
    usage: s.value / (1024 * 1024 * 1024),
    capacity: (clusterData.memoryCapacity?.[i]?.value ?? 0) / (1024 * 1024 * 1024),
    requested: (clusterData.memoryRequested?.[i]?.value ?? 0) / (1024 * 1024 * 1024),
  })) ?? [];

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 4 }}>
        <Typography variant="h4">Metrics</Typography>
        <ToggleButtonGroup size="small" value={range} exclusive onChange={(_, v) => v && setRange(v)}>
          <ToggleButton value="1h">1h</ToggleButton>
          <ToggleButton value="6h">6h</ToggleButton>
          <ToggleButton value="24h">24h</ToggleButton>
          <ToggleButton value="7d">7d</ToggleButton>
        </ToggleButtonGroup>
      </Stack>

      {metricsError && <Alert severity="error" sx={{ mb: 3 }}>{(metricsError as Error).message}</Alert>}

      {metricsLoading ? (
        <Skeleton variant="rounded" height={400} />
      ) : (
        <>
          {/* Summary cards */}
          <Grid container spacing={3} sx={{ mb: 4 }}>
            <Grid item xs={6} sm={3}>
              <Card sx={{ height: '100%' }}>
                <CardContent>
                  <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>CPU Usage</Typography>
                  <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace" }}>{cpuPct.toFixed(1)}%</Typography>
                  <LinearProgress variant="determinate" value={Math.min(cpuPct, 100)} color={cpuPct > 80 ? 'error' : cpuPct > 60 ? 'warning' : 'primary'} sx={{ mt: 1.5, height: 4, borderRadius: 2 }} />
                  <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
                    {formatCores(latestCPU)} / {formatCores(latestCPUCap)}
                  </Typography>
                </CardContent>
              </Card>
            </Grid>
            <Grid item xs={6} sm={3}>
              <Card sx={{ height: '100%' }}>
                <CardContent>
                  <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Memory Usage</Typography>
                  <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace" }}>{memPct.toFixed(1)}%</Typography>
                  <LinearProgress variant="determinate" value={Math.min(memPct, 100)} color={memPct > 80 ? 'error' : memPct > 60 ? 'warning' : 'primary'} sx={{ mt: 1.5, height: 4, borderRadius: 2 }} />
                  <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
                    {formatBytes(latestMem)} / {formatBytes(latestMemCap)}
                  </Typography>
                </CardContent>
              </Card>
            </Grid>
            <Grid item xs={6} sm={3}>
              <Card sx={{ height: '100%' }}>
                <CardContent>
                  <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Nodes</Typography>
                  <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace" }}>{nodeCount}</Typography>
                </CardContent>
              </Card>
            </Grid>
            <Grid item xs={6} sm={3}>
              <Card sx={{ height: '100%' }}>
                <CardContent>
                  <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Cost / Hour</Typography>
                  <Typography variant="h4" sx={{ fontFamily: "'Geist Mono', monospace", color: 'success.main' }}>
                    ${costData?.totalClusterCostPerHour?.toFixed(2) ?? '—'}
                  </Typography>
                  {costData?.projectedMonthlySavings && (
                    <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
                      Saving ${costData.projectedMonthlySavings.toFixed(0)}/mo
                    </Typography>
                  )}
                </CardContent>
              </Card>
            </Grid>
          </Grid>

          {/* CPU Chart */}
          <Card sx={{ mb: 3 }}>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>CPU (cores)</Typography>
              <ResponsiveContainer width="100%" height={220}>
                <AreaChart data={cpuChartData} margin={{ left: 0, right: 16 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke={theme.palette.divider} />
                  <XAxis dataKey="time" tick={{ fontSize: 11, fill: theme.palette.text.secondary }} tickFormatter={(v) => formatTimestamp(v, range)} />
                  <YAxis tick={{ fontSize: 11, fill: theme.palette.text.secondary }} />
                  <RTooltip contentStyle={{ backgroundColor: theme.palette.background.paper, border: `1px solid ${theme.palette.divider}`, borderRadius: 8, fontSize: 12 }} />
                  <Area type="monotone" dataKey="capacity" name="Capacity" stroke={theme.palette.text.disabled} fill="none" strokeDasharray="4 4" />
                  <Area type="monotone" dataKey="requested" name="Requested" stroke={theme.palette.warning.main} fill={theme.palette.warning.main} fillOpacity={0.1} />
                  <Area type="monotone" dataKey="usage" name="Usage" stroke={theme.palette.success.main} fill={theme.palette.success.main} fillOpacity={0.2} />
                </AreaChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>

          {/* Memory Chart */}
          <Card>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>Memory (GiB)</Typography>
              <ResponsiveContainer width="100%" height={220}>
                <AreaChart data={memChartData} margin={{ left: 0, right: 16 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke={theme.palette.divider} />
                  <XAxis dataKey="time" tick={{ fontSize: 11, fill: theme.palette.text.secondary }} tickFormatter={(v) => formatTimestamp(v, range)} />
                  <YAxis tick={{ fontSize: 11, fill: theme.palette.text.secondary }} />
                  <RTooltip contentStyle={{ backgroundColor: theme.palette.background.paper, border: `1px solid ${theme.palette.divider}`, borderRadius: 8, fontSize: 12 }} />
                  <Area type="monotone" dataKey="capacity" name="Capacity" stroke={theme.palette.text.disabled} fill="none" strokeDasharray="4 4" />
                  <Area type="monotone" dataKey="requested" name="Requested" stroke={theme.palette.warning.main} fill={theme.palette.warning.main} fillOpacity={0.1} />
                  <Area type="monotone" dataKey="usage" name="Usage" stroke={theme.palette.info.main} fill={theme.palette.info.main} fillOpacity={0.2} />
                </AreaChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </>
      )}
    </Box>
  );
}
