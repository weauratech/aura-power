// WorkloadCard — a unidade de leitura do Aura Power.
// Responde, sem clique: o que é, em que estado está, quanto do dia fica ligado,
// qual a próxima transição e quanto isso economiza.


import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Box from '@mui/material/Box';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';
import Switch from '@mui/material/Switch';
import Tooltip from '@mui/material/Tooltip';
import { PowerRing, type WorkloadState } from './PowerRing';
import { StatusChip } from './StatusChip';
import { WindowTimeline, type Window } from './WindowTimeline';

export interface WorkloadCardProps {
  namespace: string;
  name: string;
  kind?: string;
  state: WorkloadState;
  /** Réplicas ligadas / desejadas. */
  replicas: { current: number; desired: number };
  windows: Window[];
  now?: number;
  /** Próxima transição, já formatada (ex.: "desliga às 19:00"). */
  nextTransition?: string;
  /** Economia mensal estimada em USD. */
  savingsUsd?: number;
  governed?: boolean;
  onToggleGovernance?: (next: boolean) => void;
}

const money = (v: number) =>
  v.toLocaleString('pt-BR', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 });

export function WorkloadCard({
  namespace, name, kind = 'Deployment', state, replicas, windows, now,
  nextTransition, savingsUsd, governed = true, onToggleGovernance,
}: WorkloadCardProps) {
  const uptime = windows
    .filter(w => w.state === 'running')
    .reduce((s, w) => s + (w.to - w.from), 0) / 24;

  return (
    <Card>
      <CardContent>
        <Stack direction="row" spacing={4} alignItems="flex-start">
          <PowerRing
            value={uptime} state={state} size={40}
            label={`${Math.round(uptime * 100)}% do dia em execução`}
          />

          <Box sx={{ minWidth: 0, flex: 1 }}>
            <Stack direction="row" spacing={2} alignItems="center" sx={{ mb: 0.5 }}>
              <Typography
                variant="overline"
                sx={{ color: 'text.secondary', whiteSpace: 'nowrap' }}
              >
                {namespace}
              </Typography>
              <Typography variant="overline" sx={{ color: 'text.disabled' }}>·</Typography>
              <Typography variant="overline" sx={{ color: 'text.secondary' }}>{kind}</Typography>
            </Stack>

            <Typography
              variant="h4"
              sx={{ mb: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              title={name}
            >
              {name}
            </Typography>

            <Stack direction="row" spacing={2} alignItems="center" sx={{ mb: 4 }}>
              <StatusChip state={state} />
              <Typography variant="code" sx={{ color: 'text.secondary' }}>
                {replicas.current}/{replicas.desired} réplicas
              </Typography>
            </Stack>

            <WindowTimeline windows={windows} now={now} ariaLabel={`Janelas de ${name}`} />

            <Stack
              direction="row" spacing={4} alignItems="baseline"
              sx={{ mt: 4, pt: 4, borderTop: 1, borderColor: 'divider' }}
            >
              <Box>
                <Typography variant="overline" sx={{ color: 'text.secondary', display: 'block' }}>
                  Próxima transição
                </Typography>
                <Typography variant="body2" sx={{ mt: 0.5 }}>
                  {nextTransition ?? '—'}
                </Typography>
              </Box>
              {savingsUsd !== undefined && (
                <Box sx={{ ml: 'auto', textAlign: 'right' }}>
                  <Typography variant="overline" sx={{ color: 'text.secondary', display: 'block' }}>
                    Economia / mês
                  </Typography>
                  <Typography variant="metricSm" sx={{ mt: 0.5, display: 'block' }}>
                    {money(savingsUsd)}
                  </Typography>
                </Box>
              )}
            </Stack>
          </Box>

          <Tooltip title={governed ? 'Sob governança do Aura Power' : 'Fora da governança'}>
            <Switch
              checked={governed}
              onChange={e => onToggleGovernance?.(e.target.checked)}
              inputProps={{ 'aria-label': `Governança de ${name}` }}
            />
          </Tooltip>
        </Stack>
      </CardContent>
    </Card>
  );
}

export default WorkloadCard;
