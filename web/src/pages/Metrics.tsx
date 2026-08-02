import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Alert from '@mui/material/Alert';
import { useProviderStatus } from '../hooks/useProviderStatus';

export function Metrics() {
  const { metricsAvailable, costAvailable } = useProviderStatus();

  if (!metricsAvailable && !costAvailable) {
    return (
      <Box>
        <Typography variant="h2" sx={{ mb: 4 }}>Metrics</Typography>
        <Alert severity="info">
          No metrics provider configured. Set <code>server.prometheus.url</code> or <code>server.opencost.url</code> in your Helm values to enable cost and resource metrics.
        </Alert>
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>Metrics</Typography>
      <Card>
        <CardContent>
          <Typography variant="body2" color="text.secondary">
            Metrics integration active. Cost and resource data available via the Savings page.
          </Typography>
        </CardContent>
      </Card>
    </Box>
  );
}
