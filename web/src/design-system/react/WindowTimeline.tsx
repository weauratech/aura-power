// WindowTimeline — as 24 horas de um workload numa faixa só.
// Sólido = ligado. Pontilhado = janela dormida. É a mesma gramática do anel, esticada.


import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Tooltip from '@mui/material/Tooltip';
import { useTheme } from '@mui/material/styles';
import type { WorkloadState } from './PowerRing';

export interface Window {
  /** Hora de início, 0…24 (aceita fração: 8.5 = 08:30). */
  from: number;
  /** Hora de fim, 0…24. */
  to: number;
  state: WorkloadState;
  label?: string;
}

export interface WindowTimelineProps {
  windows: Window[];
  /** Hora atual, 0…24. Desenha o marcador de "agora". */
  now?: number;
  /** Altura da faixa em px. */
  height?: number;
  /** Mostra a régua de horas embaixo. */
  ruler?: boolean;
  ariaLabel?: string;
}

const H = 24;
const fmt = (h: number) =>
  `${String(Math.floor(h) % 24).padStart(2, '0')}:${String(Math.round((h % 1) * 60)).padStart(2, '0')}`;

export function WindowTimeline({
  windows, now, height = 10, ruler = true, ariaLabel,
}: WindowTimelineProps) {
  const theme = useTheme();
  const track = theme.palette.surface.sunken;
  const r = height / 2;

  return (
    <Box
      role="img"
      aria-label={ariaLabel ?? windows.map(w => `${fmt(w.from)}–${fmt(w.to)} ${w.state}`).join('; ')}
      sx={{ width: '100%' }}
    >
      <Box sx={{ position: 'relative', height, borderRadius: `${r}px`, backgroundColor: track }}>
        {windows.map((w, i) => {
          const left = (w.from / H) * 100;
          const width = ((w.to - w.from) / H) * 100;
          const solid = w.state === 'running' || w.state === 'failed';
          const color = theme.palette.workload[w.state];
          return (
            <Tooltip key={i} title={`${w.label ?? w.state} · ${fmt(w.from)}–${fmt(w.to)}`}>
              <Box
                sx={{
                  position: 'absolute', top: 0, height, left: `${left}%`, width: `${width}%`,
                  borderRadius: `${r}px`,
                  backgroundColor: solid ? color : 'transparent',
                  // dormindo: pontos, não bloco — a mesma leitura do arco do logo
                  ...(solid ? null : {
                    backgroundImage: `radial-gradient(circle at 2px 50%, ${color} 1.4px, transparent 1.6px)`,
                    backgroundSize: `${Math.max(6, height * 0.7)}px 100%`,
                  }),
                }}
              />
            </Tooltip>
          );
        })}
        {now !== undefined && (
          <Box
            aria-hidden
            sx={{
              position: 'absolute', top: -3, bottom: -3, left: `${(now / H) * 100}%`,
              width: 2, borderRadius: 1, backgroundColor: theme.palette.text.primary,
              transform: 'translateX(-1px)',
            }}
          />
        )}
      </Box>

      {ruler && (
        <Box sx={{ display: 'flex', justifyContent: 'space-between', mt: 1.5 }}>
          {[0, 6, 12, 18, 24].map(h => (
            <Typography key={h} variant="overline" sx={{ color: 'text.secondary', fontSize: 10 }}>
              {String(h % 24).padStart(2, '0')}h
            </Typography>
          ))}
        </Box>
      )}
    </Box>
  );
}

export default WindowTimeline;
