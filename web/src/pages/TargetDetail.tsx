import { useParams } from 'react-router-dom';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Grid from '@mui/material/Grid';
import Stack from '@mui/material/Stack';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import Chip from '@mui/material/Chip';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableRow from '@mui/material/TableRow';
import { StatusChip } from '../design-system/react';
import { PowerRing } from '../design-system/react';
import type { WorkloadState } from '../design-system/react/PowerRing';
import { useTargets, useExplainTarget } from '../hooks/useApi';

function mapState(status: { observedState: { powerState: string }; blocked: boolean; divergent: boolean }): WorkloadState {
  if (status.blocked) return 'failed';
  if (status.divergent) return 'scheduled';
  return status.observedState.powerState === 'on' ? 'running' : 'asleep';
}

export function TargetDetail() {
  const { namespace = '', name = '' } = useParams();
  const { data: targetsData } = useTargets(namespace);
  const { isLoading } = useExplainTarget(namespace, name);

  const target = targetsData?.targets?.find(
    (t) => t.spec.targetRef.name === name && t.spec.targetRef.namespace === namespace
  );

  if (isLoading) return <Skeleton variant="rounded" height={400} />;

  if (!target) {
    return <Alert severity="warning">Target not found: {namespace}/{name}</Alert>;
  }

  const state = mapState(target.status);

  return (
    <Box>
      <Stack direction="row" alignItems="center" spacing={3} sx={{ mb: 5 }}>
        <PowerRing value={state === 'running' ? 1 : 0} state={state} size={48} />
        <Box>
          <Typography variant="overline" color="text.secondary">{namespace} / {target.spec.targetRef.kind}</Typography>
          <Typography variant="h2">{name}</Typography>
        </Box>
        <Box sx={{ ml: 'auto' }}>
          <StatusChip state={state} />
        </Box>
      </Stack>

      <Grid container spacing={4}>
        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="h5" sx={{ mb: 3 }}>Status</Typography>
              <Table size="small">
                <TableBody>
                  <TableRow>
                    <TableCell><Typography variant="body2" color="text.secondary">Observed State</Typography></TableCell>
                    <TableCell><Typography variant="code">{target.status.observedState.powerState}</Typography></TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell><Typography variant="body2" color="text.secondary">Desired State</Typography></TableCell>
                    <TableCell><Typography variant="code">{target.status.desiredState || '—'}</Typography></TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell><Typography variant="body2" color="text.secondary">Replicas</Typography></TableCell>
                    <TableCell><Typography variant="code">{target.status.observedState.replicas}</Typography></TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell><Typography variant="body2" color="text.secondary">Managed</Typography></TableCell>
                    <TableCell>{target.status.managed ? <Chip label="Yes" size="small" color="success" /> : <Chip label="No" size="small" />}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell><Typography variant="body2" color="text.secondary">Divergent</Typography></TableCell>
                    <TableCell>{target.status.divergent ? <Chip label="Yes" size="small" color="warning" /> : <Chip label="No" size="small" />}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell><Typography variant="body2" color="text.secondary">Blocked</Typography></TableCell>
                    <TableCell>{target.status.blocked ? <Chip label="Yes" size="small" color="error" /> : <Chip label="No" size="small" />}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="h5" sx={{ mb: 3 }}>Decision</Typography>
              {target.status.winningRule ? (
                <Table size="small">
                  <TableBody>
                    <TableRow>
                      <TableCell><Typography variant="body2" color="text.secondary">Winning Rule</Typography></TableCell>
                      <TableCell><Typography variant="code">{target.status.winningRule.name}</Typography></TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell><Typography variant="body2" color="text.secondary">Kind</Typography></TableCell>
                      <TableCell><Chip label={target.status.winningRule.kind} size="small" /></TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell><Typography variant="body2" color="text.secondary">Priority</Typography></TableCell>
                      <TableCell><Typography variant="code">{target.status.winningRule.priority}</Typography></TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              ) : (
                <Typography variant="body2" color="text.secondary">No governing rule</Typography>
              )}
            </CardContent>
          </Card>
        </Grid>

        {target.status.savings && (
          <Grid item xs={12} md={6}>
            <Card>
              <CardContent>
                <Typography variant="h5" sx={{ mb: 3 }}>Savings</Typography>
                <Stack direction="row" spacing={6}>
                  <Box>
                    <Typography variant="overline" color="text.secondary" sx={{ display: 'block' }}>CPU Hours</Typography>
                    <Typography variant="metricSm">{target.status.savings.cpuHoursSaved.toFixed(1)}</Typography>
                  </Box>
                  <Box>
                    <Typography variant="overline" color="text.secondary" sx={{ display: 'block' }}>Memory GiB-h</Typography>
                    <Typography variant="metricSm">{target.status.savings.memoryGiBHoursSaved.toFixed(1)}</Typography>
                  </Box>
                  <Box>
                    <Typography variant="overline" color="text.secondary" sx={{ display: 'block' }}>Cost Saved</Typography>
                    <Typography variant="metricSm">${target.status.savings.estimatedCost.toFixed(2)}</Typography>
                  </Box>
                </Stack>
              </CardContent>
            </Card>
          </Grid>
        )}

        {target.status.snapshot?.available && (
          <Grid item xs={12} md={6}>
            <Card>
              <CardContent>
                <Typography variant="h5" sx={{ mb: 3 }}>Snapshot</Typography>
                <Table size="small">
                  <TableBody>
                    <TableRow>
                      <TableCell><Typography variant="body2" color="text.secondary">Replicas</Typography></TableCell>
                      <TableCell><Typography variant="code">{target.status.snapshot.replicaCount}</Typography></TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell><Typography variant="body2" color="text.secondary">CPU</Typography></TableCell>
                      <TableCell><Typography variant="code">{target.status.snapshot.resources.cpuMillicores}m</Typography></TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell><Typography variant="body2" color="text.secondary">Memory</Typography></TableCell>
                      <TableCell><Typography variant="code">{target.status.snapshot.resources.memoryMiB} MiB</Typography></TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          </Grid>
        )}
      </Grid>
    </Box>
  );
}
