import { useParams } from 'react-router-dom';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Alert from '@mui/material/Alert';
import { usePolicies } from '../hooks/useApi';
import { LoadingState } from '../components/LoadingState';
import { PageState } from '../components/PageState';

export function RuleDetail() {
  const { name } = useParams();
  const { data, isLoading, error } = usePolicies();
  const title = `Rule: ${name ?? 'unknown'}`;

  if (isLoading) {
    return <PageState title={title}><LoadingState label="Loading policy details" height={240} /></PageState>;
  }

  if (error) {
    return <PageState title={title}><Alert severity="error">{(error as Error).message}</Alert></PageState>;
  }

  const policy = data?.items?.find((p) => p.metadata.name === name);

  if (!policy) {
    return (
      <Box>
        <Typography component="h1" tabIndex={-1} variant="h2" sx={{ mb: 4 }}>{title}</Typography>
        <Typography variant="body2" color="text.secondary">Policy not found.</Typography>
      </Box>
    );
  }

  return (
    <Box>
      <Typography component="h1" tabIndex={-1} variant="h2" sx={{ mb: 4 }}>{policy.metadata.name}</Typography>
      <Card>
        <CardContent>
          <Typography variant="body2" component="pre" sx={{ fontFamily: 'mono', whiteSpace: 'pre-wrap' }}>
            {JSON.stringify(policy, null, 2)}
          </Typography>
        </CardContent>
      </Card>
    </Box>
  );
}
