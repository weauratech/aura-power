// StatusChip — o vocabulário de estado do Aura Power em uma peça.
// Regra: estado nunca é comunicado só por cor. Sempre marca + rótulo (+ forma do traço).


import Chip from '@mui/material/Chip';
import Box from '@mui/material/Box';
import { useTheme } from '@mui/material/styles';
import { light, dark } from '../tokens/tokens';
import type { WorkloadState } from './PowerRing';

export const STATE_LABEL: Record<WorkloadState, string> = {
  running: 'Running',
  asleep: 'Asleep',
  scheduled: 'Scheduled',
  failed: 'Failed',
  excluded: 'Excluded',
};

/** A forma do traço carrega o estado mesmo em preto e branco. */
export const STATE_STROKE: Record<WorkloadState, string> = {
  running: 'none', asleep: '1 5', scheduled: '3 4', failed: 'none', excluded: '6 5',
};

type Tone = { fg: string; bg: string; border: string; mark: string };

function tone(state: WorkloadState, mode: 'light' | 'dark'): Tone {
  const c = (mode === 'light' ? light : dark).color;
  const map = {
    running: [c.stateRunningFg, c.stateRunningBg, c.stateRunningBorder, c.stateRunningMark],
    asleep: [c.stateAsleepFg, c.stateAsleepBg, c.stateAsleepBorder, c.stateAsleepMark],
    scheduled: [c.stateScheduledFg, c.stateScheduledBg, c.stateScheduledBorder, c.stateScheduledMark],
    failed: [c.stateFailedFg, c.stateFailedBg, c.stateFailedBorder, c.stateFailedMark],
    excluded: [c.stateExcludedFg, c.stateExcludedBg, c.stateExcludedBorder, c.stateExcludedMark],
  }[state];
  return { fg: map[0], bg: map[1], border: map[2], mark: map[3] };
}

/** Marca circular: preenchida quando ligado, anel pontilhado quando dormindo. */
export function StatusDot({ state, size = 8 }: { state: WorkloadState; size?: number }) {
  const { mark } = tone(state, useTheme().palette.mode);
  const hollow = state === 'asleep' || state === 'excluded' || state === 'scheduled';
  return (
    <Box component="svg" viewBox="0 0 10 10" width={size} height={size}
      aria-hidden sx={{ display: 'block', flexShrink: 0 }}>
      <circle
        cx="5" cy="5" r={hollow ? 3.5 : 3.5}
        fill={hollow ? 'none' : mark}
        stroke={hollow ? mark : 'none'}
        strokeWidth={hollow ? 1.8 : 0}
        strokeDasharray={hollow ? STATE_STROKE[state] : undefined}
        strokeLinecap="round"
      />
    </Box>
  );
}

export interface StatusChipProps {
  state: WorkloadState;
  /** Sobrescreve o rótulo padrão. */
  label?: string;
  size?: 'small' | 'medium';
}

export function StatusChip({ state, label, size = 'small' }: StatusChipProps) {
  const mode = useTheme().palette.mode;
  const { fg, bg, border } = tone(state, mode);
  return (
    <Chip
      size={size}
      variant="outlined"
      icon={<StatusDot state={state} />}
      label={label ?? STATE_LABEL[state]}
      sx={{
        color: fg, backgroundColor: bg, borderColor: border,
        fontWeight: 500,
        '& .MuiChip-icon': { marginLeft: '4px', marginRight: '0px' },
      }}
    />
  );
}

export default StatusChip;
