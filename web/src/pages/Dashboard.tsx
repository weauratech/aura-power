import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Grid from '@mui/material/Grid';
import Typography from '@mui/material/Typography';
import Stack from '@mui/material/Stack';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import { useTheme } from '@mui/material/styles';
import { PieChart, Pie, Cell, ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip as RTooltip, CartesianGrid } from 'recharts';
import { PowerRing } from '../design-system/react';
import { useStatus, useTargets } from '../hooks/useApi';

interface MetricCardProps {
  label: string;
  value: string | number;
  subtitle?: string;
}

function MetricCard({ label, value, subtitle }: MetricCardProps) {
  return (
    <Card>
      <CardContent>
        <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
          {label}
        </Typography>
        <Typography variant="h3" sx={{ fontFamily: "'Geist Mono', monospace", fontVariantNumeric: 'tabular-nums' }}>
          {value}
        </Typography>
        {subtitle && (
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
            {subtitle}
          </Typography>
        )}
      </CardContent>
    </Card>
  );
}

export function Dashboard() {
  const { data, isLoading, error } = useStatus();
  const { data: targetsData } = useTargets();
  const theme = useTheme();

  if (error) {
    return <Alert severity="error">{(error as Error).message}</Alert>;
  }

  if (isLoading || !data) {
    return (
      <Grid container spacing={3}>
        {Array.from({ length: 6 }).map((_, i) => (
          <Grid item xs={12} sm={6} md={4} key={i}>
            <Skeleton variant="rounded" height={120} />
          </Grid>
        ))}
      </Grid>
    );
  }

  const total = data.totalTargets || 1;
  const onRatio = data.poweredOn / total;

  // Pie chart data
  const pieData = [
    { name: 'Powered On', value: data.poweredOn },
    { name: 'Powered Off', value: data.poweredOff },
    { name: 'Blocked', value: data.blocked },
  ];
  const pieColors = [
    theme.palette.success.main,
    theme.palette.text.disabled,
    theme.palette.error.main,
  ];

  // Namespace breakdown for bar chart
  const nsMap: Record<string, { on: number; off: number }> = {};
  targetsData?.targets?.forEach((t) => {
    const ns = t.spec.targetRef.namespace;
    if (!nsMap[ns]) nsMap[ns] = { on: 0, off: 0 };
    if (t.status.observedState.powerState === 'on') nsMap[ns].on++;
    else nsMap[ns].off++;
  });
  const barData = Object.entries(nsMap)
    .map(([ns, v]) => ({ namespace: ns, on: v.on, off: v.off }))
    .sort((a, b) => (b.on + b.off) - (a.on + a.off))
    .slice(0, 8);

  return (
    <Box>
      <Stack direction="row" alignItems="center" spacing={3} sx={{ mb: 5 }}>
        <PowerRing value={onRatio} state="running" size={48} label={`${Math.round(onRatio * 100)}% powered on`} />
        <Box>
          <Typography variant="h4">Cluster Overview</Typography>
          <Typography variant="body2" color="text.secondary">
            {data.totalTargets} targets under governance
          </Typography>
        </Box>
      </Stack>

      <Grid container spacing={3}>
        <Grid item xs={6} sm={4} md={2}>
          <MetricCard label="Powered On" value={data.poweredOn} subtitle={`${Math.round(onRatio * 100)}%`} />
        </Grid>
        <Grid item xs={6} sm={4} md={2}>
          <MetricCard label="Powered Off" value={data.poweredOff} />
        </Grid>
        <Grid item xs={6} sm={4} md={2}>
          <MetricCard label="Blocked" value={data.blocked} />
        </Grid>
        <Grid item xs={6} sm={4} md={2}>
          <MetricCard label="Divergent" value={data.divergent} />
        </Grid>
        <Grid item xs={6} sm={4} md={2}>
          <MetricCard label="Policies" value={data.activePolicies} />
        </Grid>
        <Grid item xs={6} sm={4} md={2}>
          <MetricCard label="Overrides" value={data.activeOverrides} />
        </Grid>

        {/* Power State Distribution */}
        <Grid item xs={12} md={4}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>State Distribution</Typography>
              <ResponsiveContainer width="100%" height={200}>
                <PieChart>
                  <Pie data={pieData} cx="50%" cy="50%" innerRadius={50} outerRadius={80} dataKey="value" strokeWidth={0}>
                    {pieData.map((_, i) => (
                      <Cell key={i} fill={pieColors[i]} />
                    ))}
                  </Pie>
                  <RTooltip
                    contentStyle={{
                      backgroundColor: theme.palette.background.paper,
                      border: `1px solid ${theme.palette.divider}`,
                      borderRadius: 8,
                      fontSize: 13,
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
              <Stack direction="row" spacing={3} justifyContent="center" sx={{ mt: 1 }}>
                {pieData.map((d, i) => (
                  <Stack key={d.name} direction="row" alignItems="center" spacing={1}>
                    <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: pieColors[i] }} />
                    <Typography variant="caption">{d.name}</Typography>
                  </Stack>
                ))}
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Namespace Breakdown */}
        <Grid item xs={12} md={8}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" sx={{ mb: 2 }}>Targets by Namespace</Typography>
              {barData.length > 0 ? (
                <ResponsiveContainer width="100%" height={220}>
                  <BarChart data={barData} layout="vertical" margin={{ left: 10, right: 20 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke={theme.palette.divider} />
                    <XAxis type="number" tick={{ fontSize: 12, fill: theme.palette.text.secondary }} />
                    <YAxis
                      type="category"
                      dataKey="namespace"
                      tick={{ fontSize: 11, fill: theme.palette.text.secondary }}
                      width={120}
                    />
                    <RTooltip
                      contentStyle={{
                        backgroundColor: theme.palette.background.paper,
                        border: `1px solid ${theme.palette.divider}`,
                        borderRadius: 8,
                        fontSize: 13,
                      }}
                    />
                    <Bar dataKey="on" name="On" fill={theme.palette.success.main} radius={[0, 4, 4, 0]} />
                    <Bar dataKey="off" name="Off" fill={theme.palette.text.disabled} radius={[0, 4, 4, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              ) : (
                <Typography variant="body2" color="text.secondary">No target data available</Typography>
              )}
            </CardContent>
          </Card>
        </Grid>

        {data.estimatedMonthlySavings !== undefined && data.estimatedMonthlySavings > 0 && (
          <Grid item xs={12} sm={6} md={4}>
            <Card>
              <CardContent>
                <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
                  Monthly Savings
                </Typography>
                <Typography variant="h3" sx={{ fontFamily: "'Geist Mono', monospace", color: 'success.main' }}>
                  ${data.estimatedMonthlySavings.toFixed(2)}
                </Typography>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                  Estimated cost reduction
                </Typography>
              </CardContent>
            </Card>
          </Grid>
        )}
      </Grid>
    </Box>
  );
}
