import { useState, useEffect, useId, useContext } from 'react';
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
import CircularProgress from '@mui/material/CircularProgress';
import Tooltip from '@mui/material/Tooltip';
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import CloseIcon from '@mui/icons-material/CloseOutlined';
import { useNamespaces, useTargets, apiPost, apiPut } from '../hooks/useApi';
import { useQueryClient } from '@tanstack/react-query';
import type { PowerState, TargetRef } from '../types';
import { CurrentUserContext } from '../contexts/CurrentUser';

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export interface ScheduleDrawerProps {
  open: boolean;
  onClose: () => void;
  onSuccess?: (message: string) => void;
  prefill?: {
    namespaces?: string[];
    workloadNames?: string[];
    targetRefs?: TargetRef[];
  };
  /** If provided, drawer opens in edit mode for this policy */
  editPolicy?: {
    name: string;
    namespace: string;
    spec: {
      scope: { namespaces?: string[]; workloadNames?: string[]; targetRefs?: TargetRef[] };
      schedule: { desiredState: PowerState; windows?: Array<{ start: string; end: string; timezone: string; days?: number[] }> };
      priority: number;
      description?: string;
    };
  } | null;
}

interface PreviewResult {
  totalAffected: number;
  affectedOn: TargetRef[];
  affectedOff: TargetRef[];
  blocked: Array<{ ref: TargetRef; reasons: string[] }>;
  conflicts?: Array<{ target: TargetRef }>;
}

export function ScheduleDrawer({ open, onClose, onSuccess, prefill, editPolicy }: ScheduleDrawerProps) {
  const titleId = useId();
  const currentUser = useContext(CurrentUserContext);
  const queryClient = useQueryClient();
  const { data: nsData } = useNamespaces();
  const { data: targetsData } = useTargets();

  // Scope mode
  const [scopeMode, setScopeMode] = useState<'namespaces' | 'workloads'>('namespaces');

  // Form
  const [name, setName] = useState('');
  const [selectedNamespaces, setSelectedNamespaces] = useState<string[]>([]);
  const [selectedWorkloads, setSelectedWorkloads] = useState<TargetRef[]>([]);
  const [desiredState, setDesiredState] = useState<PowerState>('off');
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
  const [previewing, setPreviewing] = useState(false);
  const [preview, setPreview] = useState<PreviewResult | null>(null);
  const [error, setError] = useState('');

  // Pre-fill from props
  useEffect(() => {
    if (prefill?.namespaces) {
      setSelectedNamespaces(prefill.namespaces);
      setScopeMode('namespaces');
    }
    if (prefill?.workloadNames) {
      const names = new Set(prefill.workloadNames);
      setSelectedWorkloads(targetsData?.targets?.map(t => t.spec.targetRef).filter(ref => names.has(`${ref.namespace}/${ref.name}`)) ?? []);
      setScopeMode('workloads');
    }
    if (prefill?.targetRefs) {
      setSelectedWorkloads(prefill.targetRefs);
      setScopeMode('workloads');
    }
  }, [prefill, targetsData?.targets]);

  // Pre-fill from editPolicy
  useEffect(() => {
    if (editPolicy) {
      setName(editPolicy.name);
      setSelectedNamespaces(editPolicy.spec.scope.namespaces || []);
      setSelectedWorkloads(editPolicy.spec.scope.targetRefs || targetsData?.targets?.map(t => t.spec.targetRef).filter(ref =>
        editPolicy.spec.scope.workloadNames?.includes(ref.name) && editPolicy.spec.scope.namespaces?.includes(ref.namespace)
      ) || []);
      setDesiredState(editPolicy.spec.schedule.desiredState);
      const win = editPolicy.spec.schedule.windows?.[0];
      if (win) {
        setStart(win.start || '20:00');
        setEnd(win.end || '08:00');
        setTimezone(win.timezone || 'America/Sao_Paulo');
        setDays(win.days || [1, 2, 3, 4, 5]);
      }
      setPriority(String(editPolicy.spec.priority));
      setDescription(editPolicy.spec.description || '');
      setScopeMode(editPolicy.spec.scope.targetRefs?.length || editPolicy.spec.scope.workloadNames?.length ? 'workloads' : 'namespaces');
    }
  }, [editPolicy, targetsData]);

  // Reset preview when form changes
  useEffect(() => {
    setPreview(null);
  }, [selectedNamespaces, selectedWorkloads, desiredState, start, end, days, priority, scopeMode]);

  const namespaceOptions = nsData?.namespaces ?? [];
  const workloadOptions = targetsData?.targets?.map(t => t.spec.targetRef) ?? [];

  const toggleDay = (d: number) => {
    setDays(prev => prev.includes(d) ? prev.filter(x => x !== d) : [...prev, d].sort());
  };

  const buildScope = () => {
    const scope: Record<string, unknown> = {};
    if (scopeMode === 'namespaces') {
      scope.namespaces = selectedNamespaces;
    } else {
      scope.targetRefs = selectedWorkloads;
    }
    return scope;
  };

  const handlePreview = async () => {
    setPreviewing(true);
    setError('');
    try {
      const scope = buildScope();
	  const previewQuery = new URLSearchParams({ name, ...(editPolicy?.namespace ? { namespace: editPolicy.namespace } : {}) });
	  const result = await apiPost<PreviewResult>(`/preview/policy?${previewQuery}`, {
        scope,
        schedule: {
          desiredState,
          windows: [{ start, end, timezone, days: days.length > 0 && days.length < 7 ? days : undefined }],
        },
        priority: parseInt(priority) || 100,
      });
      setPreview(result);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setPreviewing(false);
    }
  };

  const handleCreate = async () => {
    setCreating(true);
    setError('');
    try {
      const scope = buildScope();

      if (isOverride) {
        const expiresAt = new Date(Date.now() + parseInt(expiresIn) * 3600000).toISOString();
        const object = {
          metadata: { name },
          spec: {
            scope,
            state: desiredState,
            priority: parseInt(priority) || 500,
            expiresAt,
            reason,
            reference: reference || undefined,
          },
        };
        if (currentUser?.role === 'member') {
		  await apiPost('/pending', { action: 'create', resourceKind: 'PowerOverride', resourceName: name, payload: object });
        } else { await apiPost('/overrides', object); }
        queryClient.invalidateQueries({ queryKey: ['overrides'] });
        onSuccess?.(`Override "${name}" created (expires in ${expiresIn}h)`);
      } else if (editPolicy) {
        // Update existing policy
        await apiPut(`/policies/${editPolicy.namespace}/${editPolicy.name}`, {
          metadata: { name: editPolicy.name, namespace: editPolicy.namespace },
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
        onSuccess?.(`Schedule "${editPolicy.name}" updated`);
      } else {
        const object = {
          metadata: { name },
          spec: {
            scope,
            schedule: {
              desiredState,
              windows: [{ start, end, timezone, days: days.length > 0 && days.length < 7 ? days : undefined }],
            },
            priority: parseInt(priority) || 100,
            description,
          },
        };
        if (currentUser?.role === 'member') {
		  await apiPost('/pending', { action: 'create', resourceKind: 'PowerPolicy', resourceName: name, payload: object });
        } else { await apiPost('/policies', object); }
        queryClient.invalidateQueries({ queryKey: ['policies'] });
        onSuccess?.(currentUser?.role === 'member' ? `Schedule "${name}" submitted for approval` : `Schedule "${name}" created successfully`);
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
    setPreview(null);
  };

  const canPreview = (
    (scopeMode === 'namespaces' && selectedNamespaces.length > 0) ||
    (scopeMode === 'workloads' && selectedWorkloads.length > 0)
  ) && !isOverride;

  const canSubmit = name && (
    (scopeMode === 'namespaces' && selectedNamespaces.length > 0) ||
    (scopeMode === 'workloads' && selectedWorkloads.length > 0)
  ) && (!isOverride || reason.length >= 3);

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      PaperProps={{ role: 'dialog', 'aria-modal': true, 'aria-labelledby': titleId }}
      sx={{ '& .MuiDrawer-paper': { width: 440, maxWidth: '100vw', p: 0 } }}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* Header */}
        <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ px: 3, py: 2.5, borderBottom: 1, borderColor: 'divider' }}>
          <Typography id={titleId} variant="h5">{editPolicy ? 'Edit Schedule' : 'New Schedule'}</Typography>
          <IconButton onClick={onClose} size="small" aria-label="Close schedule drawer" autoFocus><CloseIcon /></IconButton>
        </Stack>

        {/* Body */}
        <Box sx={{ flex: 1, overflow: 'auto', px: 3, py: 3 }}>
          {error && <Alert severity="error" sx={{ mb: 3 }}>{error}</Alert>}

          <Stack spacing={3}>
            <TextField label="Name" value={name} onChange={e => setName(e.target.value)} fullWidth required size="small" />

            {/* Scope mode */}
            <Box>
              <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
                <Typography variant="overline" color="text.secondary">Scope</Typography>
                <Tooltip title="Choose whether this schedule targets entire namespaces or specific workloads">
                  <InfoOutlinedIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                </Tooltip>
              </Stack>
              <Stack role="group" aria-label="Schedule scope type" direction="row" spacing={1} sx={{ mb: 2 }}>
                <Chip label="Namespaces" size="small" variant={scopeMode === 'namespaces' ? 'filled' : 'outlined'} color={scopeMode === 'namespaces' ? 'primary' : 'default'} onClick={() => setScopeMode('namespaces')} sx={{ cursor: 'pointer' }} />
                <Chip label="Workloads" size="small" variant={scopeMode === 'workloads' ? 'filled' : 'outlined'} color={scopeMode === 'workloads' ? 'primary' : 'default'} onClick={() => setScopeMode('workloads')} sx={{ cursor: 'pointer' }} />
              </Stack>

              {scopeMode === 'namespaces' ? (
                <Autocomplete multiple size="small" options={namespaceOptions} value={selectedNamespaces} onChange={(_, v) => setSelectedNamespaces(v)} renderInput={(params) => <TextField {...params} label="Select Namespaces" placeholder="Type to search..." />} renderTags={(value, getTagProps) => value.map((option, index) => <Chip {...getTagProps({ index })} key={option} label={option} size="small" />)} />
              ) : (
                <Autocomplete multiple size="small" options={workloadOptions} value={selectedWorkloads} onChange={(_, v) => setSelectedWorkloads(v)} isOptionEqualToValue={(a, b) => workloadKey(a) === workloadKey(b)} getOptionLabel={workloadKey} renderInput={(params) => <TextField {...params} label="Select Workloads" placeholder="namespace/kind/name" />} renderTags={(value, getTagProps) => value.map((option, index) => <Chip {...getTagProps({ index })} key={workloadKey(option)} label={workloadKey(option)} size="small" />)} />
              )}
            </Box>

            <Divider />

            {/* Desired state */}
            <TextField label="Desired State" value={desiredState} onChange={e => setDesiredState(e.target.value as PowerState)} select fullWidth size="small">
              <MenuItem value="on">On — keep running during window</MenuItem>
              <MenuItem value="off">Off — power down during window</MenuItem>
            </TextField>

            {/* Override toggle */}
            <FormControlLabel
              control={<Switch checked={isOverride} onChange={e => setIsOverride(e.target.checked)} />}
              label={
                <Stack direction="row" alignItems="center" spacing={0.5}>
                  <Typography variant="body2">Temporary override</Typography>
                  <Tooltip title="Overrides expire automatically and take priority over recurring schedules">
                    <InfoOutlinedIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                  </Tooltip>
                </Stack>
              }
            />

            {isOverride ? (
              <Stack spacing={2}>
                <TextField label="Expires in (hours)" value={expiresIn} onChange={e => setExpiresIn(e.target.value)} type="number" size="small" fullWidth />
                <TextField label="Reason" value={reason} onChange={e => setReason(e.target.value)} size="small" fullWidth required multiline rows={2} helperText="Min 3 chars — justification for the exception" />
                <TextField label="Reference (optional)" value={reference} onChange={e => setReference(e.target.value)} size="small" fullWidth placeholder="JIRA-1234" />
              </Stack>
            ) : (
              <Stack spacing={2}>
                <Stack direction="row" spacing={2}>
                  <TextField label="Start" value={start} onChange={e => setStart(e.target.value)} size="small" helperText="HH:MM" sx={{ flex: 1 }} />
                  <TextField label="End" value={end} onChange={e => setEnd(e.target.value)} size="small" helperText="HH:MM" sx={{ flex: 1 }} />
                </Stack>
                <TextField label="Timezone" value={timezone} onChange={e => setTimezone(e.target.value)} size="small" fullWidth />
                <Box>
                  <Typography variant="caption" color="text.secondary" sx={{ mb: 1, display: 'block' }}>Active Days</Typography>
                  <Stack role="group" aria-label="Active days" direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                    {DAYS.map((label, i) => (
                      <Chip key={i} label={label} size="small" variant={days.includes(i) ? 'filled' : 'outlined'} color={days.includes(i) ? 'primary' : 'default'} onClick={() => toggleDay(i)} sx={{ cursor: 'pointer' }} />
                    ))}
                  </Stack>
                </Box>
                <TextField label="Description" value={description} onChange={e => setDescription(e.target.value)} size="small" fullWidth multiline rows={2} />
              </Stack>
            )}

            <Stack direction="row" alignItems="center" spacing={0.5}>
              <TextField label="Priority" value={priority} onChange={e => setPriority(e.target.value)} type="number" size="small" fullWidth />
              <Tooltip title="Higher priority wins when multiple schedules conflict. Range: 0-1000.">
                <InfoOutlinedIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
              </Tooltip>
            </Stack>

            {/* Impact Preview */}
            {!isOverride && canPreview && (
              <Box>
                <Button
                  variant="outlined"
                  size="small"
                  fullWidth
                  onClick={handlePreview}
                  disabled={previewing}
                  startIcon={previewing ? <CircularProgress size={14} /> : undefined}
                >
                  {previewing ? 'Computing...' : 'Preview Impact'}
                </Button>

                {preview && (
                  <Alert severity="info" sx={{ mt: 2 }}>
                    <Typography variant="body2" sx={{ fontWeight: 500 }}>
                      This schedule will affect {preview.totalAffected} target(s)
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {preview.affectedOff.length > 0 && `${preview.affectedOff.length} will be powered off. `}
                      {preview.affectedOn.length > 0 && `${preview.affectedOn.length} will be powered on. `}
                      {preview.blocked.length > 0 && `${preview.blocked.length} blocked by guardrails. `}
                      {(preview.conflicts?.length ?? 0) > 0 && `${preview.conflicts?.length} conflict(s) resolved by priority.`}
                    </Typography>
                  </Alert>
                )}
              </Box>
            )}
          </Stack>
        </Box>

        {/* Footer */}
        <Stack direction="row" spacing={2} sx={{ px: 3, py: 2.5, borderTop: 1, borderColor: 'divider' }}>
          <Button onClick={onClose} sx={{ flex: 1 }}>Cancel</Button>
          <Button variant="contained" onClick={handleCreate} disabled={creating || !canSubmit} sx={{ flex: 1 }}>
            {creating ? 'Saving...' : isOverride ? 'Create Override' : editPolicy ? 'Save Changes' : 'Create Schedule'}
          </Button>
        </Stack>
      </Box>
    </Drawer>
  );
}

function workloadKey(ref: TargetRef): string {
  return `${ref.namespace}/${ref.kind}/${ref.name}${ref.uid ? `#${ref.uid}` : ''}`;
}
