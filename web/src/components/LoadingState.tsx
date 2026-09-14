import Box from '@mui/material/Box';
import Skeleton from '@mui/material/Skeleton';

export function LoadingState({ label, height }: { label: string; height: number }) {
  return (
    <Box role="status" aria-live="polite" aria-label={label}>
      <Skeleton aria-hidden="true" variant="rounded" height={height} />
    </Box>
  );
}
