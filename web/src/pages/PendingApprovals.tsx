import { useState } from 'react';
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import Skeleton from '@mui/material/Skeleton';
import Stack from '@mui/material/Stack';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Typography from '@mui/material/Typography';
import { useQueryClient } from '@tanstack/react-query';
import { apiPost, usePendingApprovals } from '../hooks/useApi';

export function PendingApprovals() {
  const { data, isLoading, error } = usePendingApprovals();
  const queryClient = useQueryClient();
  const [deciding, setDeciding] = useState<string>();
  const [actionError, setActionError] = useState('');

  const decide = async (id: string, decision: 'approve' | 'reject') => {
    setDeciding(`${id}:${decision}`);
    setActionError('');
    try {
      await apiPost(`/pending/${id}/${decision}`, {});
      await queryClient.invalidateQueries({ queryKey: ['pending'] });
    } catch (requestError) {
      setActionError((requestError as Error).message);
    } finally {
      setDeciding(undefined);
    }
  };

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 1 }}>Pending Approvals</Typography>
      <Typography color="text.secondary" sx={{ mb: 4 }}>
        Review the exact operation and payload before applying it to the cluster.
      </Typography>
      {actionError && <Alert severity="error" sx={{ mb: 2 }}>{actionError}</Alert>}
      {isLoading ? <Skeleton variant="rounded" height={240} /> : data?.items.length ? (
        <TableContainer>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Request</TableCell>
                <TableCell>Resource</TableCell>
                <TableCell>Requested by</TableCell>
                <TableCell>Details</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {data.items.map(change => (
                <TableRow key={change.id}>
                  <TableCell><Chip size="small" label={change.action} /></TableCell>
                  <TableCell>
                    <Typography variant="subtitle2">{change.resourceName}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {change.resourceKind} · {change.resourceNamespace}
                      {change.resourceVersion ? ` · revision ${change.resourceVersion}` : ''}
                    </Typography>
                  </TableCell>
                  <TableCell>{change.username}</TableCell>
                  <TableCell>
                    <details>
                      <summary>Review payload</summary>
                      <Box component="pre" sx={{ maxWidth: 420, overflow: 'auto', whiteSpace: 'pre-wrap', fontSize: 12 }}>
                        {change.payload || '(delete operation)'}
                      </Box>
                    </details>
                  </TableCell>
                  <TableCell align="right">
                    <Stack direction="row" spacing={1} justifyContent="flex-end">
                      <Button size="small" color="error" variant="outlined" disabled={Boolean(deciding)} onClick={() => decide(change.id, 'reject')}>
                        Reject {change.resourceName}
                      </Button>
                      <Button size="small" variant="contained" disabled={Boolean(deciding)} onClick={() => decide(change.id, 'approve')}>
                        Approve {change.resourceName}
                      </Button>
                    </Stack>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      ) : <Alert severity="info">No pending approval requests.</Alert>}
    </Box>
  );
}
