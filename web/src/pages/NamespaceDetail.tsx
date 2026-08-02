import { useParams } from 'react-router-dom';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { Targets } from './Targets';

export function NamespaceDetail() {
  const { namespace } = useParams();
  return (
    <Box>
      <Typography variant="overline" color="text.secondary" sx={{ mb: 1, display: 'block' }}>Namespace</Typography>
      <Typography variant="h2" sx={{ mb: 4 }}>{namespace}</Typography>
      <Targets />
    </Box>
  );
}
