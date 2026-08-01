import { Box, HStack, Text, VStack, useToast, Button, Drawer, DrawerOverlay, DrawerContent, DrawerCloseButton, DrawerHeader, DrawerBody, DrawerFooter, FormControl, FormLabel, Input, Select, Textarea, Checkbox, Stack, NumberInput, NumberInputField, FormErrorMessage, Wrap, WrapItem, Tag, TagLabel, TagCloseButton, Divider, Switch, FormHelperText, Alert, AlertIcon } from '@chakra-ui/react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useNamespaces, useNamespaceGroups, PolicyResponse } from '../hooks/useApi';

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const SYSTEM_NS = ['kube-system', 'kube-public', 'kube-node-lease', 'aura-system'];

interface Props {
  isOpen: boolean;
  onClose: () => void;
  editPolicy?: PolicyResponse | null;
}

export function CreateRuleDrawer({ isOpen, onClose, editPolicy }: Props) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { data: nsData } = useNamespaces();
  const { data: groupsData } = useNamespaceGroups();

  const [isOverride, setIsOverride] = useState(false);
  const [form, setForm] = useState({
    name: '',
    selectedNamespaces: [] as string[],
    start: '08:00',
    end: '18:00',
    timezone: 'America/Sao_Paulo',
    days: [1, 2, 3, 4, 5] as number[],
    priority: 10,
    description: '',
    // Override fields
    target: '',
    state: 'on',
    duration: '3h',
    reason: '',
    reference: '',
  });
  const [errors, setErrors] = useState<Record<string, string>>({});

  const namespaces = (nsData?.namespaces ?? []).filter(ns => !SYSTEM_NS.includes(ns)).sort();

  useEffect(() => {
    if (editPolicy) {
      const w = editPolicy.spec.schedule.windows?.[0];
      setForm(f => ({
        ...f,
        name: editPolicy.metadata.name,
        selectedNamespaces: editPolicy.spec.scope.namespaces ?? [],
        start: w?.start ?? '08:00',
        end: w?.end ?? '18:00',
        timezone: w?.timezone ?? 'America/Sao_Paulo',
        days: w?.days ?? [1, 2, 3, 4, 5],
        priority: editPolicy.spec.priority,
        description: editPolicy.spec.description ?? '',
      }));
      setIsOverride(false);
    }
  }, [editPolicy]);

  const createMutation = useMutation({
    mutationFn: async () => {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };

      if (!isOverride) {
        const body = {
          apiVersion: 'power.aura.sh/v1alpha1',
          kind: 'PowerPolicy',
          metadata: { name: form.name, namespace: 'aura-system' },
          spec: {
            scope: { namespaces: form.selectedNamespaces },
            schedule: {
              desiredState: 'on',
              windows: [{ start: form.start, end: form.end, timezone: form.timezone, days: form.days }],
            },
            priority: form.priority,
            description: form.description,
          },
        };

        if (editPolicy) {
          // UPDATE existing policy
          const r = await fetch(`/api/v1/policies/aura-system/${form.name}`, {
            method: 'PUT',
            credentials: 'same-origin',
            headers,
            body: JSON.stringify(body),
          });
          if (!r.ok) { const e = await r.json(); throw new Error(e.error || 'Failed to update policy'); }
        } else {
          // CREATE new policy
          const r = await fetch('/api/v1/policies', {
            method: 'POST',
            credentials: 'same-origin',
            headers,
            body: JSON.stringify(body),
          });
          if (!r.ok) { const e = await r.json(); throw new Error(e.error || 'Failed to create policy'); }
        }
      } else {
        const durationMs = parseDuration(form.duration);
        const parts = form.target.split('/');
        const body = {
          apiVersion: 'power.aura.sh/v1alpha1',
          kind: 'PowerOverride',
          metadata: { name: `override-${Date.now()}`, namespace: 'aura-system' },
          spec: {
            scope: { namespaces: [parts[0]], ...(parts[1] ? { workloadNames: [parts[1]] } : {}) },
            state: form.state,
            priority: form.priority,
            expiresAt: new Date(Date.now() + durationMs).toISOString(),
            reason: form.reason,
            reference: form.reference || undefined,
          },
        };
        const r = await fetch('/api/v1/overrides', { method: 'POST', credentials: 'same-origin', headers, body: JSON.stringify(body) });
        if (!r.ok) { const e = await r.json(); throw new Error(e.error || 'Failed to create override'); }
      }
    },
    onSuccess: () => {
      const policyName = form.name;
      toast({ title: editPolicy ? 'Policy updated' : `${isOverride ? 'Override' : 'Policy'} created`, status: 'success', duration: 3000 });
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      queryClient.invalidateQueries({ queryKey: ['overrides'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      queryClient.invalidateQueries({ queryKey: ['targets'] });
      resetAndClose();
      if (!isOverride) {
        navigate(`/rules/${policyName}`);
      }
    },
    onError: (err: Error) => toast({ title: 'Failed', description: err.message, status: 'error', duration: 5000 }),
  });

  const validate = (): boolean => {
    const e: Record<string, string> = {};
    if (!isOverride) {
      if (!form.name) e.name = 'Required';
      else if (!/^[a-z][a-z0-9-]*$/.test(form.name)) e.name = 'Lowercase, starts with letter, only a-z, 0-9, hyphens';
      if (form.selectedNamespaces.length === 0) e.namespaces = 'Select at least one namespace';
      if (!/^\d{2}:\d{2}$/.test(form.start)) e.start = 'Invalid time';
      if (!/^\d{2}:\d{2}$/.test(form.end)) e.end = 'Invalid time';
      if (form.days.length === 0) e.days = 'Select at least one day';
    } else {
      if (!form.target) e.target = 'Required';
      if (!form.reason || form.reason.length < 3) e.reason = 'At least 3 characters';
    }
    setErrors(e);
    return Object.keys(e).length === 0;
  };

  const resetAndClose = () => {
    setForm({ name: '', selectedNamespaces: [], start: '08:00', end: '18:00', timezone: 'America/Sao_Paulo', days: [1, 2, 3, 4, 5], priority: 10, description: '', target: '', state: 'on', duration: '3h', reason: '', reference: '' });
    setErrors({});
    setIsOverride(false);
    onClose();
  };

  const toggleDay = (day: number) => {
    setForm(f => ({ ...f, days: f.days.includes(day) ? f.days.filter(d => d !== day) : [...f.days, day].sort() }));
  };

  const toggleNamespace = (ns: string) => {
    setForm(f => ({ ...f, selectedNamespaces: f.selectedNamespaces.includes(ns) ? f.selectedNamespaces.filter(n => n !== ns) : [...f.selectedNamespaces, ns] }));
  };

  return (
    <Drawer isOpen={isOpen} onClose={resetAndClose} size="md" placement="right">
      <DrawerOverlay />
      <DrawerContent>
        <DrawerCloseButton />
        <DrawerHeader borderBottomWidth={1}>
          <VStack align="start" spacing={1}>
            <Text fontSize="lg" fontWeight="bold">{editPolicy ? 'Edit Power Policy' : isOverride ? 'New Temporary Override' : 'New Power Policy'}</Text>
            <Text fontSize="sm" color="gray.500" fontWeight="normal">
              {editPolicy ? 'Modify an existing policy schedule' : isOverride ? 'Exception with automatic expiration' : 'Recurring schedule for workload governance'}
            </Text>
          </VStack>
        </DrawerHeader>

        <DrawerBody pt={4}>
          <VStack spacing={5} align="stretch">
            {/* Toggle */}
            {!editPolicy && (
              <HStack justify="space-between" p={3} bg="gray.50" borderRadius="md">
                <VStack align="start" spacing={0}>
                  <Text fontSize="sm" fontWeight="medium">Temporary override?</Text>
                  <Text fontSize="xs" color="gray.500">Overrides expire automatically</Text>
                </VStack>
                <Switch isChecked={isOverride} onChange={e => setIsOverride(e.target.checked)} colorScheme="orange" />
              </HStack>
            )}

            <Divider />

            {!isOverride ? (
              <>
                {/* POLICY NAME */}
                <FormControl isRequired isInvalid={!!errors.name}>
                  <FormLabel fontSize="sm">Policy name</FormLabel>
                  <Input size="sm" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="dev-off-hours" isReadOnly={!!editPolicy} bg={editPolicy ? 'gray.100' : undefined} />
                  <FormErrorMessage fontSize="xs">{errors.name}</FormErrorMessage>
                </FormControl>

                {/* NAMESPACE MULTI-SELECT */}
                <FormControl isRequired isInvalid={!!errors.namespaces}>
                  <FormLabel fontSize="sm">Namespaces to govern</FormLabel>
                  {/* Quick-select from groups */}
                  {(groupsData?.items ?? []).length > 0 && (
                    <HStack mb={2} spacing={2} flexWrap="wrap">
                      <Text fontSize="xs" color="gray.500">Groups:</Text>
                      {(groupsData?.items ?? []).map(g => (
                        <Button key={g.metadata.name} size="xs" variant="outline" colorScheme="blue"
                          onClick={() => setForm(f => ({ ...f, selectedNamespaces: [...new Set([...f.selectedNamespaces, ...g.spec.namespaces])] }))}>
                          {g.metadata.name} ({g.spec.namespaces.length})
                        </Button>
                      ))}
                    </HStack>
                  )}
                  <HStack mb={2} spacing={2}>
                    <Button size="xs" variant="link" colorScheme="blue" onClick={() => setForm(f => ({ ...f, selectedNamespaces: [...namespaces] }))}>Select all</Button>
                    <Button size="xs" variant="link" onClick={() => setForm(f => ({ ...f, selectedNamespaces: [] }))}>Deselect all</Button>
                  </HStack>
                  <Box maxH="160px" overflowY="auto" borderWidth={1} borderRadius="md" p={2}>
                    <Stack spacing={1}>
                      {namespaces.map(ns => (
                        <Checkbox key={ns} size="sm" isChecked={form.selectedNamespaces.includes(ns)} onChange={() => toggleNamespace(ns)}>
                          <Text fontSize="sm">{ns}</Text>
                        </Checkbox>
                      ))}
                      {namespaces.length === 0 && <Text fontSize="xs" color="gray.400">Loading namespaces...</Text>}
                    </Stack>
                  </Box>
                  {form.selectedNamespaces.length > 0 && (
                    <Wrap mt={2} spacing={1}>
                      {form.selectedNamespaces.map(ns => (
                        <WrapItem key={ns}>
                          <Tag size="sm" colorScheme="blue" borderRadius="full">
                            <TagLabel>{ns}</TagLabel>
                            <TagCloseButton onClick={() => toggleNamespace(ns)} />
                          </Tag>
                        </WrapItem>
                      ))}
                    </Wrap>
                  )}
                  <FormErrorMessage fontSize="xs">{errors.namespaces}</FormErrorMessage>
                </FormControl>

                <Divider />

                {/* TIME WINDOW - using type="time" for native validation */}
                <FormControl isInvalid={!!errors.start || !!errors.end}>
                  <FormLabel fontSize="sm">Active window (workloads ON during this time)</FormLabel>
                  <HStack>
                    <Input size="sm" type="time" value={form.start} onChange={e => setForm({ ...form, start: e.target.value })} />
                    <Text fontSize="sm" color="gray.500" px={1}>to</Text>
                    <Input size="sm" type="time" value={form.end} onChange={e => setForm({ ...form, end: e.target.value })} />
                  </HStack>
                  <FormHelperText fontSize="xs">Outside this window, workloads will be powered OFF</FormHelperText>
                  {errors.start && <Text fontSize="xs" color="red.500">{errors.start}</Text>}
                </FormControl>

                {/* DAY PICKER */}
                <FormControl isInvalid={!!errors.days}>
                  <FormLabel fontSize="sm">Active days</FormLabel>
                  <HStack spacing={1}>
                    {WEEKDAYS.map((day, i) => (
                      <Button
                        key={i}
                        size="sm"
                        w="42px"
                        h="36px"
                        variant={form.days.includes(i) ? 'solid' : 'outline'}
                        colorScheme={form.days.includes(i) ? 'blue' : 'gray'}
                        onClick={() => toggleDay(i)}
                        fontSize="xs"
                      >
                        {day}
                      </Button>
                    ))}
                  </HStack>
                  <HStack mt={1} spacing={2}>
                    <Button size="xs" variant="link" onClick={() => setForm(f => ({ ...f, days: [1, 2, 3, 4, 5] }))}>Weekdays</Button>
                    <Button size="xs" variant="link" onClick={() => setForm(f => ({ ...f, days: [0, 1, 2, 3, 4, 5, 6] }))}>Every day</Button>
                    <Button size="xs" variant="link" onClick={() => setForm(f => ({ ...f, days: [0, 6] }))}>Weekends</Button>
                  </HStack>
                  <FormErrorMessage fontSize="xs">{errors.days}</FormErrorMessage>
                </FormControl>

                {/* TIMEZONE */}
                <FormControl>
                  <FormLabel fontSize="sm">Timezone</FormLabel>
                  <Select size="sm" value={form.timezone} onChange={e => setForm({ ...form, timezone: e.target.value })}>
                    <option value="America/Sao_Paulo">America/Sao_Paulo (BRT)</option>
                    <option value="UTC">UTC</option>
                    <option value="America/New_York">America/New_York (EST)</option>
                    <option value="America/Chicago">America/Chicago (CST)</option>
                    <option value="Europe/London">Europe/London (GMT)</option>
                    <option value="Europe/Berlin">Europe/Berlin (CET)</option>
                    <option value="Asia/Tokyo">Asia/Tokyo (JST)</option>
                  </Select>
                </FormControl>

                {/* PRIORITY */}
                <FormControl>
                  <FormLabel fontSize="sm">Priority</FormLabel>
                  <NumberInput size="sm" value={form.priority} onChange={(_, v) => setForm({ ...form, priority: v || 0 })} min={0} max={1000}>
                    <NumberInputField />
                  </NumberInput>
                  <FormHelperText fontSize="xs">Higher priority wins when rules conflict (0-1000)</FormHelperText>
                </FormControl>

                {/* DESCRIPTION */}
                <FormControl>
                  <FormLabel fontSize="sm">Description (optional)</FormLabel>
                  <Textarea size="sm" value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} placeholder="What this policy does and why" rows={2} />
                </FormControl>
              </>
            ) : (
              <>
                {/* OVERRIDE: TARGET */}
                <FormControl isRequired isInvalid={!!errors.target}>
                  <FormLabel fontSize="sm">Target</FormLabel>
                  <Select size="sm" value={form.target} onChange={e => setForm({ ...form, target: e.target.value })} placeholder="Select namespace...">
                    {namespaces.map(ns => (
                      <option key={ns} value={ns}>{ns} (all workloads)</option>
                    ))}
                  </Select>
                  <FormHelperText fontSize="xs">Or type a specific workload: namespace/name</FormHelperText>
                  <FormErrorMessage fontSize="xs">{errors.target}</FormErrorMessage>
                </FormControl>

                {/* STATE + DURATION */}
                <HStack align="start">
                  <FormControl isRequired>
                    <FormLabel fontSize="sm">Desired state</FormLabel>
                    <Select size="sm" value={form.state} onChange={e => setForm({ ...form, state: e.target.value })}>
                      <option value="on">Power On (keep alive)</option>
                      <option value="off">Power Off (shut down)</option>
                    </Select>
                  </FormControl>
                  <FormControl isRequired>
                    <FormLabel fontSize="sm">Duration</FormLabel>
                    <Select size="sm" value={form.duration} onChange={e => setForm({ ...form, duration: e.target.value })}>
                      <option value="30m">30 minutes</option>
                      <option value="1h">1 hour</option>
                      <option value="2h">2 hours</option>
                      <option value="3h">3 hours</option>
                      <option value="6h">6 hours</option>
                      <option value="12h">12 hours</option>
                      <option value="24h">24 hours</option>
                      <option value="48h">48 hours</option>
                    </Select>
                  </FormControl>
                </HStack>

                {/* REASON */}
                <FormControl isRequired isInvalid={!!errors.reason}>
                  <FormLabel fontSize="sm">Reason</FormLabel>
                  <Textarea size="sm" value={form.reason} onChange={e => setForm({ ...form, reason: e.target.value })} placeholder="Why is this override needed?" rows={2} />
                  <FormErrorMessage fontSize="xs">{errors.reason}</FormErrorMessage>
                </FormControl>

                {/* REFERENCE + PRIORITY */}
                <HStack>
                  <FormControl>
                    <FormLabel fontSize="sm">Reference</FormLabel>
                    <Input size="sm" value={form.reference} onChange={e => setForm({ ...form, reference: e.target.value })} placeholder="JIRA-1234" />
                  </FormControl>
                  <FormControl>
                    <FormLabel fontSize="sm">Priority</FormLabel>
                    <NumberInput size="sm" value={form.priority} onChange={(_, v) => setForm({ ...form, priority: v || 100 })} min={0} max={1000}>
                      <NumberInputField />
                    </NumberInput>
                  </FormControl>
                </HStack>

                <Alert status="info" borderRadius="md" fontSize="xs">
                  <AlertIcon boxSize={4} />
                  Override expires automatically after the selected duration. No permanent exceptions.
                </Alert>
              </>
            )}
          </VStack>
        </DrawerBody>

        <DrawerFooter borderTopWidth={1}>
          <Button variant="ghost" mr={3} onClick={resetAndClose}>Cancel</Button>
          <Button
            colorScheme={isOverride ? 'orange' : 'blue'}
            onClick={() => { if (validate()) createMutation.mutate(); }}
            isLoading={createMutation.isPending}
          >
            {editPolicy ? 'Save Changes' : isOverride ? 'Create Override' : 'Create Policy'}
          </Button>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  );
}

function parseDuration(s: string): number {
  const match = s.match(/^(\d+)(m|h)$/);
  if (!match) return 3 * 3600000;
  const val = parseInt(match[1]);
  return match[2] === 'h' ? val * 3600000 : val * 60000;
}
