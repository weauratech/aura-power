import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Grid from '@mui/material/Grid';
import Stack from '@mui/material/Stack';
import Button from '@mui/material/Button';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import LinearProgress from '@mui/material/LinearProgress';
import { useSavings, useTargets } from '../hooks/useApi';

export function Savings() {
  const { data, isLoading, error } = useSavings();
  const { data: targetsData } = useTargets();

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;
  if (isLoading) return <Skeleton variant="rounded" height={200} />;

  // Build per-target savings breakdown
  const targetSavings = targetsData?.targets
    ?.filter(t => t.status.savings && t.status.savings.estimatedCost > 0)
    ?.sort((a, b) => (b.status.savings?.estimatedCost ?? 0) - (a.status.savings?.estimatedCost ?? 0))
    ?? [];

  const maxCost = targetSavings.length > 0 ? (targetSavings[0].status.savings?.estimatedCost ?? 1) : 1;

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 4 }}>
        <Typography variant="h4">Savings</Typography>
        <Button variant="outlined" size="small" href="/api/v1/savings/export" download>
          Export CSV
        </Button>
      </Stack>

      <Grid container spacing={3} sx={{ mb: 5 }}>
        <Grid item xs={12} sm={4}>
          <Card>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
                CPU Hours Saved
              </Typography>
              <Typography variant="h3" sx={{ fontFamily: "'Geist Mono', monospace" }}>
                {data?.totalCPUHours?.toFixed(1) ?? '0'}
              </Typography>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                vCPU-hours not consumed
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={4}>
          <Card>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
                Memory GiB-Hours
              </Typography>
              <Typography variant="h3" sx={{ fontFamily: "'Geist Mono', monospace" }}>
                {data?.totalMemoryGiB?.toFixed(1) ?? '0'}
              </Typography>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                GiB-hours not allocated
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={4}>
          <Card>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
                Estimated Cost Saved
              </Typography>
              <Typography variant="h3" sx={{ fontFamily: "'Geist Mono', monospace", color: 'success.main' }}>
                ${data?.totalEstimatedCost?.toFixed(2) ?? '0.00'}
              </Typography>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                Total accumulated
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Per-target breakdown */}
      {targetSavings.length > 0 && (
        <Box>
          <Typography variant="h5" sx={{ mb: 3 }}>Breakdown by Target</Typography>
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Target</TableCell>
                  <TableCell>CPU-h</TableCell>
                  <TableCell>Mem GiB-h</TableCell>
                  <TableCell>Cost</TableCell>
                  <TableCell sx={{ width: '30%' }} />
                </TableRow>
              </TableHead>
              <TableBody>
                {targetSavings.map((t) => {
                  const s = t.status.savings!;
                  const pct = (s.estimatedCost / maxCost) * 100;
                  return (
                    <TableRow key={`${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`} hover>
                      <TableCell>
                        <Typography variant="subtitle2">{t.spec.targetRef.name}</Typography>
                        <Typography variant="caption" color="text.secondary">{t.spec.targetRef.namespace}</Typography>
                      </TableCell>
                      <TableCell><Typography variant="code">{s.cpuHoursSaved.toFixed(1)}</Typography></TableCell>
                      <TableCell><Typography variant="code">{s.memoryGiBHoursSaved.toFixed(1)}</Typography></TableCell>
                      <TableCell><Typography variant="code">${s.estimatedCost.toFixed(2)}</Typography></TableCell>
                      <TableCell>
                        <LinearProgress variant="determinate" value={pct} sx={{ height: 6, borderRadius: 3 }} />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </TableContainer>
        </Box>
      )}

      {targetSavings.length === 0 && (
        <Alert severity="info" sx={{ mt: 2 }}>
          Savings accumulate as workloads are powered off. Power down some targets to see per-target breakdown.
        </Alert>
      )}
    </Box>
  );
}
