import { Link } from 'react-router-dom';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import MuiLink from '@mui/material/Link';
import { StatusChip } from '../design-system/react';
import type { WorkloadState } from '../design-system/react/PowerRing';
import { useTargets } from '../hooks/useApi';

function mapState(target: { status: { observedState: { powerState: string }; blocked: boolean; divergent: boolean } }): WorkloadState {
  if (target.status.blocked) return 'failed';
  if (target.status.divergent) return 'scheduled';
  return target.status.observedState.powerState === 'on' ? 'running' : 'asleep';
}

export function Targets() {
  const { data, isLoading, error } = useTargets();

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>Targets</Typography>

      {isLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Namespace</TableCell>
                <TableCell>Name</TableCell>
                <TableCell>Kind</TableCell>
                <TableCell>State</TableCell>
                <TableCell>Desired</TableCell>
                <TableCell align="right">Replicas</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {data?.targets?.map((t) => (
                <TableRow key={`${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`} hover>
                  <TableCell>
                    <Typography variant="code" color="text.secondary">
                      {t.spec.targetRef.namespace}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <MuiLink
                      component={Link}
                      to={`/targets/${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`}
                      underline="hover"
                    >
                      {t.spec.targetRef.name}
                    </MuiLink>
                  </TableCell>
                  <TableCell>
                    <Typography variant="code" color="text.secondary">
                      {t.spec.targetRef.kind}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <StatusChip state={mapState(t)} />
                  </TableCell>
                  <TableCell>
                    <Typography variant="code">
                      {t.status.desiredState || '—'}
                    </Typography>
                  </TableCell>
                  <TableCell align="right">
                    <Typography variant="code">
                      {t.status.observedState.replicas}
                    </Typography>
                  </TableCell>
                </TableRow>
              ))}
              {(!data?.targets || data.targets.length === 0) && (
                <TableRow>
                  <TableCell colSpan={6} align="center">
                    <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>
                      No targets discovered yet
                    </Typography>
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Box>
  );
}
