import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Chip from '@mui/material/Chip';
import { usePolicies } from '../hooks/useApi';

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export function Schedule() {
  const { data, isLoading, error } = usePolicies();

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>Schedule</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 4 }}>
        Active time windows defined by policies.
      </Typography>

      {isLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Policy</TableCell>
                <TableCell>State</TableCell>
                <TableCell>Window</TableCell>
                <TableCell>Days</TableCell>
                <TableCell>Timezone</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {data?.items?.flatMap((p) =>
                (p.spec.schedule.windows || []).map((w, i) => (
                  <TableRow key={`${p.metadata.name}-${i}`} hover>
                    <TableCell><Typography variant="subtitle2">{p.metadata.name}</Typography></TableCell>
                    <TableCell><Chip label={p.spec.schedule.desiredState} size="small" /></TableCell>
                    <TableCell>
                      <Typography variant="code">{w.start} — {w.end}</Typography>
                    </TableCell>
                    <TableCell>
                      {w.days?.map((d) => (
                        <Chip key={d} label={DAYS[d]} size="small" variant="outlined" sx={{ mr: 0.5 }} />
                      )) || <Chip label="Every day" size="small" variant="outlined" />}
                    </TableCell>
                    <TableCell><Typography variant="code">{w.timezone}</Typography></TableCell>
                  </TableRow>
                ))
              )}
              {(!data?.items || data.items.length === 0) && (
                <TableRow>
                  <TableCell colSpan={5} align="center">
                    <Typography variant="body2" color="text.secondary" sx={{ py: 4 }}>
                      No schedules defined
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
