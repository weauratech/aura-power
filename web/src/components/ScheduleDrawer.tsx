import { useState, useEffect } from 'react';
import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import MenuItem from '@mui/material/MenuItem';
import Stack from '@mui/material/Stack';
import Chip from '@mui/material/Chip';
import Alert from '@mui/material/Alert';
import Divider from '@mui/material/Divider';
import Switch from '@mui/material/Switch';
import FormControlLabel from '@mui/material/FormControlLabel';
import Autocomplete from '@mui/material/Autocomplete';
import IconButton from '@mui/material/IconButton';
import CloseIcon from '@mui/icons-material/CloseOutlined';
import { useNamespaces, useTargets, apiPost } from '../hooks/useApi';
import { useQueryClient } from '@tanstack/react-query';

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export interface ScheduleDrawerProps {
  open: boolean;
  onClose: () => void;
  /** Pre-fill scope */
  prefill?: {
    namespaces?: string[];
    workloadNames?: string[];
  };
}

export function ScheduleDrawer({ open, onClose, prefill }: ScheduleDrawerProps) {
  const queryClient = useQueryClient();
  const { data: nsData } = useNamespaces();
  const { data: targetsData } = useTargets();

  // Scope mode
  const [scopeMode, setScopeMode] = useState<'namespaces' | 'workloads'>('namespaces');

  // Form
  const [name, setName] = useState('');
  const [selectedNamespaces, setSelectedNamespaces] = useState<string[]>([]);
  const [selectedWorkloads, setSelectedWorkloads] = useState<string[]>([]);
  const [desiredState, setDesiredState] = useState('off');
  const [start, setStart] = useState('20:00');
  const [end, setEnd] = useState('08:00');
  const [timezone, setTimezone] = useState('America/Sao_Paulo');
  const [days, setDays] = useState<number[]>([1, 2, 3, 4, 5]);
  const [priority, setPriority] = useState('100');
  const [description, setDescription] = useState('');

  // Override toggle
  const [isOverride, setIsOverride] = useState(false);
  const [expiresIn, setExpiresIn] = useState('4');
  const [reason, setReason] = useState('');
  const [reference, setReference] = useState('');

  // State
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');

  // Pre-fill from props
  useEffect(() => {
    if (prefill?.namespaces) {
      setSelectedNamespaces(prefill.namespaces);
      setScopeMode('namespaces');
    }
    if (prefill?.workloadNames) {
      setSelectedWorkloads(prefill.workloadNames);
      setScopeMode('workloads');
    }
  }, [prefill]);

  const namespaceOptions = nsData?.namespaces ?? [];
  const workloadOptions = targetsData?.targets?.map(t => `${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`) ?? [];

  const toggleDay = (d: number) => {
    setDays(prev => prev.includes(d) ? prev.filter(x => x !== d) : [...prev, d].sort());
  };

  const handleCreate = async () => {
    setCreating(true);
    setError('');
    try {
      const scope: Record<string, unknown> = {};
      if (scopeMode === 'namespaces') {
        scope.namespaces = selectedNamespaces;
      } else {
        // Extract namespace from "ns/name" format
        const nsSet = new Set(selectedWorkloads.map(w => w.split('/')[0]));
        scope.namespaces = Array.from(nsSet);
        scope.workloadNames = selectedWorkloads.map(w => w.split('/')[1]);
      }

      if (isOverride) {
        const expiresAt = new Date(Date.now() + parseInt(expiresIn) * 3600000).toISOString();
        await apiPost('/overrides', {
          metadata: { name, namespace: 'aura-system' },
          spec: {
            scope,
            state: desiredState,
            priority: parseInt(priority) || 500,
            expiresAt,
            reason,
            reference: reference || undefined,
          },
        });
        queryClient.invalidateQueries({ queryKey: ['overrides'] });
      } else {
        await apiPost('/policies', {
          metadata: { name, namespace: 'aura-system' },
          spec: {
            scope,
            schedule: {
              desiredState,
              windows: [{ start, end, timezone, days: days.length > 0 && days.length < 7 ? days : undefined }],
            },
            priority: parseInt(priority) || 100,
            description,
          },
        });
        queryClient.invalidateQueries({ queryKey: ['policies'] });
      }

      onClose();
      resetForm();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreating(false);
    }
  };

  const resetForm = () => {
    setName(''); setSelectedNamespaces([]); setSelectedWorkloads([]);
    setDesiredState('off'); setStart('20:00'); setEnd('08:00');
    setTimezone('America/Sao_Paulo'); setDays([1, 2, 3, 4, 5]);
    setPriority('100'); setDescription(''); setIsOverride(false);
    setExpiresIn('4'); setReason(''); setReference(''); setError('');
  };

  const canSubmit = name && (
    (scopeMode === 'namespaces' && selectedNamespaces.length > 0) ||
    (scopeMode === 'workloads' && selectedWorkloads.length > 0)
  ) && (!isOverride || reason.length >= 3);

  return (
    <Drawer anchor="right" open={open} onClose={onClose} sx={{ '& .MuiDrawer-paper': { width: 420, p: 0 } }}>
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* Header */}
        <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ px: 3, py: 2.5, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="h5">New Schedule</Typography>
          <IconButton onClick={onClose} size="small"><CloseIcon /></IconButton>
        </Stack>

        {/* Body */}
        <Box sx={{ flex: 1, overflow: 'auto', px: 3, py: 3 }}>
          {error && <Alert severity="error" sx={{ mb: 3 }}>{error}</Alert>}

          <Stack spacing={3}>
            <TextField label="Name" value={name} onChange={e => setName(e.target.value)} fullWidth required size="small" />

            {/* Scope mode */}
            <Box>
              <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Scope</Typography>
              <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
                <Chip
                  label="Namespaces"
                  size="small"
                  variant={scopeMode === 'namespaces' ? 'filled' : 'outlined'}
                  color={scopeMode === 'namespaces' ? 'primary' : 'default'}
                  onClick={() => setScopeMode('namespaces')}
                  sx={{ cursor: 'pointer' }}
                />
                <Chip
                  label="Workloads"
                  size="small"
                  variant={scopeMode === 'workloads' ? 'filled' : 'outlined'}
                  color={scopeMode === 'workloads' ? 'primary' : 'default'}
                  onClick={() => setScopeMode('workloads')}
                  sx={{ cursor: 'pointer' }}
                />
              </Stack>

              {scopeMode === 'namespaces' ? (
                <Autocomplete
                  multiple
                  size="small"
                  options={namespaceOptions}
                  value={selectedNamespaces}
                  onChange={(_, v) => setSelectedNamespaces(v)}
                  renderInput={(params) => <TextField {...params} label="Select Namespaces" placeholder="Type to search..." />}
                  renderTags={(value, getTagProps) =>
                    value.map((option, index) => (
                      <Chip {...getTagProps({ index })} key={option} label={option} size="small" />
                    ))
                  }
                />
              ) : (
                <Autocomplete
                  multiple
                  size="small"
                  options={workloadOptions}
                  value={selectedWorkloads}
                  onChange={(_, v) => setSelectedWorkloads(v)}
                  renderInput={(params) => <TextField {...params} label="Select Workloads" placeholder="namespace/name" />}
                  renderTags={(value, getTagProps) =>
                    value.map((option, index) => (
                      <Chip {...getTagProps({ index })} key={option} label={option} size="small" />
                    ))
                  }
                />
              )}
            </Box>

            <Divider />

            {/* Desired state */}
            <TextField label="Desired State" value={desiredState} onChange={e => setDesiredState(e.target.value)} select fullWidth size="small">
              <MenuItem value="on">On — keep running during window</MenuItem>
              <MenuItem value="off">Off — power down during window</MenuItem>
            </TextField>

            {/* Override toggle */}
            <FormControlLabel
              control={<Switch checked={isOverride} onChange={e => setIsOverride(e.target.checked)} />}
              label={<Typography variant="body2">Temporary override (expires automatically)</Typography>}
            />

            {isOverride ? (
              /* Override fields */
              <Stack spacing={2}>
                <TextField label="Expires in (hours)" value={expiresIn} onChange={e => setExpiresIn(e.target.value)} type="number" size="small" fullWidth />
                <TextField label="Reason" value={reason} onChange={e => setReason(e.target.value)} size="small" fullWidth required multiline rows={2} helperText="Min 3 chars — justification" />
                <TextField label="Reference (optional)" value={reference} onChange={e => setReference(e.target.value)} size="small" fullWidth placeholder="JIRA-1234" />
              </Stack>
            ) : (
              /* Schedule fields */
              <Stack spacing={2}>
                <Stack direction="row" spacing={2}>
                  <TextField label="Start" value={start} onChange={e => setStart(e.target.value)} size="small" helperText="HH:MM" sx={{ flex: 1 }} />
                  <TextField label="End" value={end} onChange={e => setEnd(e.target.value)} size="small" helperText="HH:MM" sx={{ flex: 1 }} />
                </Stack>
                <TextField label="Timezone" value={timezone} onChange={e => setTimezone(e.target.value)} size="small" fullWidth />
                <Box>
                  <Typography variant="caption" color="text.secondary" sx={{ mb: 1, display: 'block' }}>Active Days</Typography>
                  <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                    {DAYS.map((label, i) => (
                      <Chip
                        key={i}
                        label={label}
                        size="small"
                        variant={days.includes(i) ? 'filled' : 'outlined'}
                        color={days.includes(i) ? 'primary' : 'default'}
                        onClick={() => toggleDay(i)}
                        sx={{ cursor: 'pointer' }}
                      />
                    ))}
                  </Stack>
                </Box>
                <TextField label="Description" value={description} onChange={e => setDescription(e.target.value)} size="small" fullWidth multiline rows={2} />
              </Stack>
            )}

            <TextField label="Priority" value={priority} onChange={e => setPriority(e.target.value)} type="number" size="small" fullWidth helperText="Higher wins (0-1000)" />
          </Stack>
        </Box>

        {/* Footer */}
        <Stack direction="row" spacing={2} sx={{ px: 3, py: 2.5, borderTop: 1, borderColor: 'divider' }}>
          <Button onClick={onClose} sx={{ flex: 1 }}>Cancel</Button>
          <Button variant="contained" onClick={handleCreate} disabled={creating || !canSubmit} sx={{ flex: 1 }}>
            {creating ? 'Creating...' : isOverride ? 'Create Override' : 'Create Schedule'}
          </Button>
        </Stack>
      </Box>
    </Drawer>
  );
}
