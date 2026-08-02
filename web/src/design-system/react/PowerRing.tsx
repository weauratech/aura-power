// PowerRing — o logo "Janela" virando indicador de estado.
// Sólido = a fatia do ciclo em execução. Pontilhado = a janela dormida.
// Em `value={0.5}` o componente É o logo: metade sólida à direita, metade em pontos à esquerda.


import { useTheme } from '@mui/material/styles';
import Box from '@mui/material/Box';

export type WorkloadState = 'running' | 'asleep' | 'scheduled' | 'failed' | 'excluded';

export interface PowerRingProps {
  /** Fração do ciclo em execução, 0…1. */
  value?: number;
  /** Estado do workload — define a cor do arco sólido. */
  state?: WorkloadState;
  /** Diâmetro em px. Mínimo legível: 16. */
  size?: number;
  /** Espessura do traço; por padrão escala com o tamanho (5/64 do quadro). */
  thickness?: number;
  /** Mostra a haste central do símbolo de power. */
  stem?: boolean;
  /** Rótulo acessível. Sem ele o anel vira decorativo (aria-hidden). */
  label?: string;
}

const GAP = 60;          // abertura no topo, em graus — a mesma do logo
const SWEEP = 360 - GAP; // 300°
const START = 90 - GAP / 2;
const R = 20;
const C = 32;

const pol = (deg: number, r = R) => {
  const a = (deg * Math.PI) / 180;
  return [C + r * Math.cos(a), C + 1.4 - r * Math.sin(a)] as const;
};

/** Arco no sentido horário da tela (graus decrescentes). */
function arcCW(from: number, sweep: number, r = R) {
  const [x0, y0] = pol(from, r);
  const [x1, y1] = pol(from - sweep, r);
  return `M ${x0.toFixed(3)} ${y0.toFixed(3)} A ${r} ${r} 0 ${sweep > 180 ? 1 : 0} 1 ${x1.toFixed(3)} ${y1.toFixed(3)}`;
}

export function PowerRing({
  value = 1, state = 'running', size = 24, thickness, stem = true, label,
}: PowerRingProps) {
  const theme = useTheme();
  const v = Math.max(0, Math.min(1, value));
  const sw = thickness ?? 5;
  const on = theme.palette.workload[state];
  const off = theme.palette.workload.asleep;
  const ink = theme.palette.text.primary;

  const onSweep = SWEEP * v;
  const offSweep = SWEEP - onSweep;
  // circunferência do trecho dormido, para cravar os pontos em passo inteiro
  const offLen = (offSweep / 360) * 2 * Math.PI * R;
  const step = offLen > 0 ? offLen / Math.max(2, Math.round(offLen / 5.2)) : 1;

  return (
    <Box
      component="svg"
      viewBox="0 0 64 64"
      width={size}
      height={size}
      role={label ? 'img' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      sx={{ display: 'block', flexShrink: 0 }}
    >
      {offSweep > 0.5 && (
        <path
          d={arcCW(START - onSweep, offSweep)}
          fill="none" stroke={off} strokeWidth={sw} strokeLinecap="round"
          strokeDasharray={`0.01 ${step.toFixed(3)}`}
        />
      )}
      {onSweep > 0.5 && (
        <path
          d={arcCW(START, onSweep)}
          fill="none" stroke={on} strokeWidth={sw} strokeLinecap="round"
        />
      )}
      {stem && (
        <path
          d="M 32 11 V 31" fill="none" stroke={state === 'asleep' ? off : ink}
          strokeWidth={sw + 0.4} strokeLinecap="round"
        />
      )}
    </Box>
  );
}

export default PowerRing;
