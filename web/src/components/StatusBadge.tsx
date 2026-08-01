import { Badge } from '@chakra-ui/react';

interface StatusBadgeProps {
  state: string;
}

export function StatusBadge({ state }: StatusBadgeProps) {
  const colorScheme = getColorScheme(state);
  const label = getLabel(state);
  return (
    <Badge colorScheme={colorScheme} data-testid={`status-badge-${state}`}>
      {label}
    </Badge>
  );
}

function getLabel(state: string): string {
  switch (state) {
    case 'on': return 'On';
    case 'off': return 'Off';
    case 'blocked': return 'Blocked';
    case 'divergent': return 'Divergent';
    case 'unmanaged': return 'Unmanaged';
    case 'unknown': return 'Unknown';
    default: return state || 'Unmanaged';
  }
}

function getColorScheme(state: string): string {
  switch (state) {
    case 'on':
      return 'green';
    case 'off':
      return 'red';
    case 'blocked':
      return 'red';
    case 'divergent':
      return 'orange';
    case 'unmanaged':
      return 'gray';
    case 'unknown':
      return 'gray';
    default:
      return 'gray';
  }
}
