import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

export function PageState({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Box>
      <Typography component="h1" tabIndex={-1} variant="h4" sx={{ mb: 4 }}>{title}</Typography>
      {children}
    </Box>
  );
}
