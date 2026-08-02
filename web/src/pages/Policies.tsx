import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Chip from '@mui/material/Chip';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import { StatusChip } from '../design-system/react';
import type { WorkloadState } from '../design-system/react/PowerRing';
import { usePolicies } from '../hooks/useApi';

export function Policies() {
  const { data, isLoading, error } = usePolicies();

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>Policies</Typography>

      {isLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>Desired State</TableCell>
                <TableCell>Priority</TableCell>
                <TableCell>Scope</TableCell>
                <TableCell align="right">Affected Targets</TableCell>
                <TableCell>Description</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {data?.items?.map((p) => {
                const state: WorkloadState = p.spec.schedule.desiredState === 'on' ? 'running' : 'asleep';
                return (
                  <TableRow key={`${p.metadata.namespace}/${p.metadata.name}`} hover>
                    <TableCell>
                      <Typography variant="subtitle2">{p.metadata.name}</Typography>
                    </TableCell>
                    <TableCell>
                      <StatusChip state={state} label={p.spec.schedule.desiredState} />
                    </TableCell>
                    <TableCell>
                      <Typography variant="code">{p.spec.priority}</Typography>
                    </TableCell>
                    <TableCell>
                      {p.spec.scope.namespaces?.map((ns) => (
                        <Chip key={ns} label={ns} size="small" variant="outlined" sx={{ mr: 0.5, mb: 0.5 }} />
                      ))}
                    </TableCell>
                    <TableCell align="right">
                      <Typography variant="code">{p.status?.affectedTargets ?? 0}</Typography>
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" color="text.secondary">
                        {p.spec.description || '—'}
                      </Typography>
                    </TableCell>
                  </TableRow>
                );
              })}
              {(!data?.items || data.items.length === 0) && (
                <TableRow>
                  <TableCell colSpan={6} align="center">
                    <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>
                      No policies configured
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
