import { useParams } from 'react-router-dom';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import { usePolicies } from '../hooks/useApi';

export function RuleDetail() {
  const { name } = useParams();
  const { data } = usePolicies();

  const policy = data?.items?.find((p) => p.metadata.name === name);

  if (!policy) {
    return (
      <Box>
        <Typography variant="h2" sx={{ mb: 4 }}>Rule: {name}</Typography>
        <Typography variant="body2" color="text.secondary">Policy not found.</Typography>
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h2" sx={{ mb: 4 }}>{policy.metadata.name}</Typography>
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
