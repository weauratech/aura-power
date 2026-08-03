import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Stack from '@mui/material/Stack';
import { PowerRing } from '../design-system/react';

interface EmptyStateProps {
  title: string;
  description: string;
  actionLabel?: string;
  onAction?: () => void;
  icon?: 'ring' | 'none';
}

export function EmptyState({ title, description, actionLabel, onAction, icon = 'ring' }: EmptyStateProps) {
  return (
    <Box sx={{ py: 10, textAlign: 'center' }}>
      <Stack alignItems="center" spacing={3}>
        {icon === 'ring' && (
          <PowerRing value={0} state="asleep" size={56} stem={false} />
        )}
        <Box>
          <Typography variant="h5" sx={{ mb: 1 }}>{title}</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ maxWidth: 400, mx: 'auto' }}>
            {description}
          </Typography>
        </Box>
        {actionLabel && onAction && (
          <Button variant="contained" onClick={onAction}>
            {actionLabel}
          </Button>
        )}
      </Stack>
    </Box>
  );
}
