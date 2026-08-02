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
import { useTargets } from '../hooks/useApi';

export function Blocked() {
  const { data, isLoading, error } = useTargets();

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  const blocked = data?.targets?.filter((t) => t.status.blocked) ?? [];

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>Blocked Targets</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 4 }}>
        Workloads where guardrails prevent power actions.
      </Typography>

      {isLoading ? (
        <Skeleton variant="rounded" height={300} />
      ) : blocked.length === 0 ? (
        <Alert severity="success">No blocked targets. All workloads are operating normally.</Alert>
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Namespace</TableCell>
                <TableCell>Name</TableCell>
                <TableCell>Block Reasons</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {blocked.map((t) => (
                <TableRow key={`${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`} hover>
                  <TableCell><Typography variant="code">{t.spec.targetRef.namespace}</Typography></TableCell>
                  <TableCell><Typography variant="subtitle2">{t.spec.targetRef.name}</Typography></TableCell>
                  <TableCell>
                    {t.status.blockReasons?.map((r, i) => (
                      <Chip
                        key={i}
                        label={`${r.type}: ${r.message}`}
                        size="small"
                        color={r.waivable ? 'warning' : 'error'}
                        sx={{ mr: 0.5, mb: 0.5 }}
                      />
                    ))}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Box>
  );
}
