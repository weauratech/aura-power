import { Box, Heading, Table, Thead, Tbody, Tr, Th, Td, Badge, Spinner, IconButton, HStack, Text, VStack, useToast, AlertDialog, AlertDialogOverlay, AlertDialogContent, AlertDialogHeader, AlertDialogBody, AlertDialogFooter, Button, useDisclosure, Card, CardBody, Tabs, TabList, Tab, TabPanels, TabPanel, Wrap, WrapItem, Flex } from '@chakra-ui/react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { usePolicies, useOverrides, PolicyResponse } from '../hooks/useApi';
import { CreateRuleDrawer } from '../components/CreateRuleDrawer';

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export function Policies() {
  const queryClient = useQueryClient();
  const toast = useToast();
  const { data: policiesData, isLoading: loadingPolicies } = usePolicies();
  const { data: overridesData, isLoading: loadingOverrides } = useOverrides();
  const { isOpen: isDrawerOpen, onOpen: openDrawer, onClose: closeDrawer } = useDisclosure();
  const { isOpen: isDeleteOpen, onOpen: openDelete, onClose: closeDelete } = useDisclosure();
  const [deleteTarget, setDeleteTarget] = useState<{ kind: string; name: string; namespace: string } | null>(null);
  const [editPolicy, setEditPolicy] = useState<PolicyResponse | null>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);

  const deleteMutation = useMutation({
    mutationFn: async ({ kind, namespace, name }: { kind: string; namespace: string; name: string }) => {
      const endpoint = kind === 'policy' ? 'policies' : 'overrides';
      const r = await fetch(`/api/v1/${endpoint}/${namespace}/${name}`, { method: 'DELETE' });
      if (!r.ok) throw new Error('Failed');
    },
    onSuccess: () => {
      toast({ title: 'Deleted', status: 'success', duration: 2000 });
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      queryClient.invalidateQueries({ queryKey: ['overrides'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      closeDelete();
    },
    onError: () => toast({ title: 'Delete failed', status: 'error', duration: 3000 }),
  });

  const handleDelete = (kind: string, name: string, namespace: string) => {
    setDeleteTarget({ kind, name, namespace });
    openDelete();
  };

  const handleEdit = (policy: PolicyResponse) => {
    setEditPolicy(policy);
    openDrawer();
  };

  const handleDrawerClose = () => {
    setEditPolicy(null);
    closeDrawer();
  };

  if (loadingPolicies || loadingOverrides) return <Flex justify="center" align="center" py={12}><Spinner size="lg" color="blue.500" /><Text ml={3} color="gray.500">Loading rules...</Text></Flex>;

  const policies = policiesData?.items ?? [];
  const overrides = overridesData?.items ?? [];
  const activeOverrides = overrides.filter(o => !o.status?.phase || o.status.phase === 'Active');
  const expiredOverrides = overrides.filter(o => o.status?.phase === 'Expired');

  return (
    <VStack spacing={6} align="stretch">
      <HStack justify="space-between">
        <Box>
          <Heading size="lg">Power Rules</Heading>
          <Text color="gray.500" fontSize="sm" mt={1}>Policies and overrides controlling workload power state</Text>
        </Box>
        <Button colorScheme="blue" onClick={openDrawer}>+ New Rule</Button>
      </HStack>

      <Tabs variant="enclosed" colorScheme="blue">
        <TabList borderBottomWidth="2px" borderBottomColor="gray.200">
          <Tab _selected={{ color: 'blue.600', borderBottomColor: 'blue.500', borderBottomWidth: '2px', fontWeight: 'semibold' }}>Policies ({policies.length})</Tab>
          <Tab _selected={{ color: 'blue.600', borderBottomColor: 'blue.500', borderBottomWidth: '2px', fontWeight: 'semibold' }}>Active Overrides ({activeOverrides.length})</Tab>
          {expiredOverrides.length > 0 && <Tab _selected={{ color: 'blue.600', borderBottomColor: 'blue.500', borderBottomWidth: '2px', fontWeight: 'semibold' }}>Expired ({expiredOverrides.length})</Tab>}
        </TabList>
        <TabPanels>
          <TabPanel px={0} pt={4}>
            {policies.length === 0 ? (
              <EmptyState msg="No policies yet" sub='Click "+ New Rule" to create a recurring schedule' />
            ) : (
              <Box bg="white" borderRadius="lg" shadow="sm" overflowX="auto">
                <Table size="sm">
                  <Thead>
                    <Tr>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Name</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Scope</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Schedule</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Priority</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Created</Th>
                      <Th w="80px"></Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {policies.map(p => {
                      const w = p.spec.schedule.windows?.[0];
                      return (
                        <Tr key={p.metadata.name} cursor="pointer" _hover={{ bg: 'gray.50' }} transition="all 0.2s">
                          <Td>
                            <VStack align="start" spacing={0}>
                              <Link to={`/rules/${p.metadata.name}`}>
                                <Text fontWeight="medium" color="blue.600" _hover={{ textDecoration: 'underline' }}>{p.metadata.name}</Text>
                              </Link>
                              {p.spec.description && <Text fontSize="xs" color="gray.500" noOfLines={1}>{p.spec.description}</Text>}
                            </VStack>
                          </Td>
                          <Td>
                            <Wrap spacing={1}>
                              {(p.spec.scope.namespaces || ['All']).map(ns => (
                                <WrapItem key={ns}><Badge variant="subtle" colorScheme="blue" fontSize="xs">{ns}</Badge></WrapItem>
                              ))}
                            </Wrap>
                          </Td>
                          <Td>{w ? <Text fontSize="sm" color="gray.700">{w.start}&ndash;{w.end} {formatDays(w.days || [])}</Text> : <Text fontSize="sm" color="gray.700">Always</Text>}</Td>
                          <Td><Badge variant="subtle" colorScheme="purple" fontSize="xs">{p.spec.priority}</Badge></Td>
                          <Td fontSize="xs" color="gray.500">{new Date(p.metadata.creationTimestamp).toLocaleDateString()}</Td>
                          <Td>
                            <HStack spacing={1}>
                              <IconButton aria-label="Edit" icon={<Text fontSize="sm">{'\u270E'}</Text>} size="xs" variant="ghost" colorScheme="blue" onClick={() => handleEdit(p)} data-testid={`edit-policy-${p.metadata.name}`} />
                              <IconButton aria-label="Delete" icon={<Text fontSize="sm">{'\u2715'}</Text>} size="xs" variant="ghost" colorScheme="red" onClick={() => handleDelete('policy', p.metadata.name, p.metadata.namespace)} data-testid={`delete-policy-${p.metadata.name}`} />
                            </HStack>
                          </Td>
                        </Tr>
                      );
                    })}
                  </Tbody>
                </Table>
              </Box>
            )}
          </TabPanel>

          <TabPanel px={0} pt={4}>
            {activeOverrides.length === 0 ? (
              <EmptyState msg="No active overrides" sub="Overrides are temporary exceptions that expire automatically" />
            ) : (
              <Box bg="white" borderRadius="lg" shadow="sm" overflowX="auto">
                <Table size="sm">
                  <Thead>
                    <Tr>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Target</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">State</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Expires</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Reason</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Priority</Th>
                      <Th w="50px"></Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {activeOverrides.map(o => (
                      <Tr key={o.metadata.name} _hover={{ bg: 'gray.50' }} transition="all 0.2s">
                        <Td fontWeight="medium" color="gray.700">{[...(o.spec.scope.namespaces || []), ...(o.spec.scope.workloadNames || [])].join('/')}</Td>
                        <Td><Badge variant="subtle" colorScheme={o.spec.state === 'on' ? 'green' : 'gray'}>{o.spec.state}</Badge></Td>
                        <Td fontSize="sm" color="gray.700">{new Date(o.spec.expiresAt).toLocaleString()}</Td>
                        <Td fontSize="sm" noOfLines={1} maxW="200px" color="gray.600">{o.spec.reason}</Td>
                        <Td><Badge variant="subtle" colorScheme="purple" fontSize="xs">{o.spec.priority}</Badge></Td>
                        <Td><IconButton aria-label="Delete" icon={<Text fontSize="sm">{'\u2715'}</Text>} size="xs" variant="ghost" colorScheme="red" onClick={() => handleDelete('override', o.metadata.name, o.metadata.namespace)} /></Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            )}
          </TabPanel>

          {expiredOverrides.length > 0 && (
            <TabPanel px={0} pt={4}>
              <Box bg="white" borderRadius="lg" shadow="sm" overflowX="auto" opacity={0.7}>
                <Table size="sm">
                  <Thead>
                    <Tr>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Target</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">State</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Expired At</Th>
                      <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Reason</Th>
                      <Th w="50px"></Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {expiredOverrides.map(o => (
                      <Tr key={o.metadata.name}>
                        <Td color="gray.600">{[...(o.spec.scope.namespaces || []), ...(o.spec.scope.workloadNames || [])].join('/')}</Td>
                        <Td><Badge variant="outline">{o.spec.state}</Badge></Td>
                        <Td fontSize="sm" color="gray.500">{new Date(o.spec.expiresAt).toLocaleString()}</Td>
                        <Td fontSize="sm" noOfLines={1} color="gray.500">{o.spec.reason}</Td>
                        <Td><IconButton aria-label="Delete" icon={<Text fontSize="sm">{'\u2715'}</Text>} size="xs" variant="ghost" onClick={() => handleDelete('override', o.metadata.name, o.metadata.namespace)} /></Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            </TabPanel>
          )}
        </TabPanels>
      </Tabs>

      <CreateRuleDrawer isOpen={isDrawerOpen} onClose={handleDrawerClose} editPolicy={editPolicy} />

      <AlertDialog isOpen={isDeleteOpen} leastDestructiveRef={cancelRef} onClose={closeDelete}>
        <AlertDialogOverlay><AlertDialogContent borderRadius="lg">
          <AlertDialogHeader>Delete {deleteTarget?.kind}</AlertDialogHeader>
          <AlertDialogBody>Delete <strong>{deleteTarget?.name}</strong>? This cannot be undone.</AlertDialogBody>
          <AlertDialogFooter>
            <Button ref={cancelRef} onClick={closeDelete}>Cancel</Button>
            <Button colorScheme="red" onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)} ml={3} isLoading={deleteMutation.isPending}>Delete</Button>
          </AlertDialogFooter>
        </AlertDialogContent></AlertDialogOverlay>
      </AlertDialog>
    </VStack>
  );
}

function EmptyState({ msg, sub }: { msg: string; sub: string }) {
  return (
    <Card shadow="sm" borderRadius="lg">
      <CardBody textAlign="center" py={12}>
        <Text color="gray.500" mb={1} fontWeight="medium">{msg}</Text>
        <Text fontSize="sm" color="gray.400">{sub}</Text>
      </CardBody>
    </Card>
  );
}

function formatDays(days: number[]): string {
  if (days.length === 0 || days.length === 7) return '';
  const sorted = [...days].sort();
  if (JSON.stringify(sorted) === JSON.stringify([1, 2, 3, 4, 5])) return '(Mon-Fri)';
  if (JSON.stringify(sorted) === JSON.stringify([0, 6])) return '(Weekends)';
  return `(${days.map(d => WEEKDAYS[d]).join(', ')})`;
}
