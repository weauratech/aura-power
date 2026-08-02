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
import { useOverrides } from '../hooks/useApi';

export function Overrides() {
  const { data, isLoading, error } = useOverrides();

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>Overrides</Typography>

      {isLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>State</TableCell>
                <TableCell>Priority</TableCell>
                <TableCell>Phase</TableCell>
                <TableCell>Expires In</TableCell>
                <TableCell>Reason</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {data?.items?.map((o) => {
                const state: WorkloadState = o.spec.state === 'on' ? 'running' : 'asleep';
                return (
                  <TableRow key={`${o.metadata.namespace}/${o.metadata.name}`} hover>
                    <TableCell><Typography variant="subtitle2">{o.metadata.name}</Typography></TableCell>
                    <TableCell><StatusChip state={state} label={o.spec.state} /></TableCell>
                    <TableCell><Typography variant="code">{o.spec.priority}</Typography></TableCell>
                    <TableCell>
                      <Chip
                        label={o.status?.phase || 'Active'}
                        size="small"
                        color={o.status?.phase === 'Expired' ? 'default' : 'success'}
                      />
                    </TableCell>
                    <TableCell><Typography variant="code">{o.status?.expiresIn || '—'}</Typography></TableCell>
                    <TableCell><Typography variant="body2" color="text.secondary">{o.spec.reason}</Typography></TableCell>
                  </TableRow>
                );
              })}
              {(!data?.items || data.items.length === 0) && (
                <TableRow>
                  <TableCell colSpan={6} align="center">
                    <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>No overrides active</Typography>
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
