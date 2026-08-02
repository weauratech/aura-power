// SchedulesTable — a lista de agendamentos. Cron e alvo em mono; estado com marca + rótulo.


import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Typography from '@mui/material/Typography';
import Stack from '@mui/material/Stack';
import Box from '@mui/material/Box';
import { StatusChip } from './StatusChip';
import { WindowTimeline, type Window } from './WindowTimeline';
import type { WorkloadState } from './PowerRing';

export interface ScheduleRow {
  id: string;
  name: string;
  target: string;
  cronDown: string;
  cronUp: string;
  timezone: string;
  windows: Window[];
  state: WorkloadState;
  workloads: number;
  savingsUsd: number;
}

export interface SchedulesTableProps {
  rows: ScheduleRow[];
  now?: number;
  caption?: string;
}

const money = (v: number) =>
  v.toLocaleString('pt-BR', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 });

export function SchedulesTable({ rows, now, caption }: SchedulesTableProps) {
  return (
    <TableContainer>
      <Table size="small" aria-label={caption ?? 'Agendamentos'}>
        <TableHead>
          <TableRow>
            <TableCell>Agendamento</TableCell>
            <TableCell>Alvo</TableCell>
            <TableCell>Cron</TableCell>
            <TableCell sx={{ minWidth: 180 }}>Janela</TableCell>
            <TableCell align="right">Workloads</TableCell>
            <TableCell align="right">Economia / mês</TableCell>
            <TableCell>Estado</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map(r => (
            <TableRow key={r.id} hover>
              <TableCell>
                <Typography variant="subtitle2">{r.name}</Typography>
              </TableCell>
              <TableCell>
                <Typography variant="code" sx={{ color: 'text.secondary' }}>{r.target}</Typography>
              </TableCell>
              <TableCell>
                <Stack spacing={0.5}>
                  <Typography variant="code" sx={{ color: 'text.secondary' }}>
                    ↓ {r.cronDown}
                  </Typography>
                  <Typography variant="code" sx={{ color: 'text.secondary' }}>
                    ↑ {r.cronUp}
                  </Typography>
                  <Typography variant="caption" sx={{ color: 'text.disabled' }}>
                    {r.timezone}
                  </Typography>
                </Stack>
              </TableCell>
              <TableCell>
                <Box sx={{ py: 1 }}>
                  <WindowTimeline
                    windows={r.windows} now={now} height={8} ruler={false}
                    ariaLabel={`Janela de ${r.name}`}
                  />
                </Box>
              </TableCell>
              <TableCell align="right">
                <Typography variant="code">{r.workloads}</Typography>
              </TableCell>
              <TableCell align="right">
                <Typography variant="code">{money(r.savingsUsd)}</Typography>
              </TableCell>
              <TableCell>
                <StatusChip state={r.state} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

export default SchedulesTable;
