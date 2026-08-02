import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Grid from '@mui/material/Grid';
import Stack from '@mui/material/Stack';
import Skeleton from '@mui/material/Skeleton';
import Alert from '@mui/material/Alert';
import { useSavings } from '../hooks/useApi';

export function Savings() {
  const { data, isLoading, error } = useSavings();

  if (error) return <Alert severity="error">{(error as Error).message}</Alert>;
  if (isLoading) return <Skeleton variant="rounded" height={200} />;

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>Savings</Typography>

      <Grid container spacing={4}>
        <Grid item xs={12} sm={4}>
          <Card>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 2 }}>
                CPU Hours Saved
              </Typography>
              <Typography variant="metric">{data?.totalCPUHours?.toFixed(1) ?? '0'}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                vCPU-hours not consumed
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={4}>
          <Card>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 2 }}>
                Memory GiB-Hours
              </Typography>
              <Typography variant="metric">{data?.totalMemoryGiB?.toFixed(1) ?? '0'}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                GiB-hours not allocated
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={4}>
          <Card>
            <CardContent>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 2 }}>
                Estimated Cost
              </Typography>
              <Stack direction="row" alignItems="baseline" spacing={1}>
                <Typography variant="metric">${data?.totalEstimatedCost?.toFixed(2) ?? '0.00'}</Typography>
              </Stack>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                Total savings accumulated
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}
