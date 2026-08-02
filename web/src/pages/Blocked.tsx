import { useState } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Collapse from '@mui/material/Collapse';
import IconButton from '@mui/material/IconButton';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import KeyboardArrowRightIcon from '@mui/icons-material/KeyboardArrowRight';
import { useTargets } from '../hooks/useApi';
import type { PowerTarget } from '../types';

function BlockedRow({ target }: { target: PowerTarget }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <TableRow hover sx={{ '& td': { borderBottom: open ? 0 : undefined } }}>
        <TableCell sx={{ width: 40 }}>
          <IconButton size="small" onClick={() => setOpen(!open)}>
            {open ? <KeyboardArrowDownIcon fontSize="small" /> : <KeyboardArrowRightIcon fontSize="small" />}
          </IconButton>
        </TableCell>
        <TableCell><Typography variant="code" color="text.secondary">{target.spec.targetRef.namespace}</Typography></TableCell>
        <TableCell><Typography variant="subtitle2">{target.spec.targetRef.name}</Typography></TableCell>
        <TableCell><Typography variant="code" color="text.secondary">{target.spec.targetRef.kind}</Typography></TableCell>
        <TableCell align="right">
          <Chip label={`${target.status.blockReasons?.length ?? 0} reason(s)`} size="small" color="warning" variant="outlined" />
        </TableCell>
      </TableRow>
      <TableRow>
        <TableCell colSpan={5} sx={{ py: 0, px: 0 }}>
          <Collapse in={open} timeout="auto" unmountOnExit>
            <Box sx={{ py: 2, px: 6 }}>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1.5 }}>Block Reasons</Typography>
              <Stack spacing={1.5}>
                {target.status.blockReasons?.map((r, i) => (
                  <Box key={i} sx={{ p: 2, borderRadius: 1, border: 1, borderColor: 'divider', bgcolor: 'background.paper' }}>
                    <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 0.5 }}>
                      <Chip label={r.type} size="small" color={r.waivable ? 'warning' : 'error'} sx={{ height: 22, fontSize: 11 }} />
                      {r.waivable && <Chip label="Waivable" size="small" variant="outlined" sx={{ height: 22, fontSize: 11 }} />}
                    </Stack>
                    <Typography variant="body2" sx={{ mt: 1 }}>{r.message}</Typography>
                  </Box>
                ))}
              </Stack>
            </Box>
          </Collapse>
        </TableCell>
      </TableRow>
    </>
  );
}

export function Blocked() {
  const { data, isLoading, error } = useTargets();

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;

  const blocked = data?.targets?.filter((t) => t.status.blocked) ?? [];

  return (
    <Box>
      <Typography variant="h4" sx={{ mb: 1 }}>Blocked Targets</Typography>
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
                <TableCell />
                <TableCell>Namespace</TableCell>
                <TableCell>Name</TableCell>
                <TableCell>Kind</TableCell>
                <TableCell align="right">Blocks</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {blocked.map((t) => (
                <BlockedRow key={`${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`} target={t} />
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Box>
  );
}
