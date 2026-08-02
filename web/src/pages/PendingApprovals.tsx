import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Alert from '@mui/material/Alert';

export function PendingApprovals() {
  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>Pending Approvals</Typography>
      <Alert severity="info">No pending approval requests.</Alert>
    </Box>
  );
}
