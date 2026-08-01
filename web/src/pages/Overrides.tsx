import { Box, Heading, Table, Thead, Tbody, Tr, Th, Td, Badge, Spinner, Alert, AlertIcon, IconButton, HStack, Text, VStack, useToast, AlertDialog, AlertDialogOverlay, AlertDialogContent, AlertDialogHeader, AlertDialogBody, AlertDialogFooter, Button, useDisclosure, Card, CardBody } from '@chakra-ui/react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';

interface OverrideItem {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: {
    scope: { namespaces?: string[]; workloadNames?: string[] };
    state: string;
    priority: number;
    expiresAt: string;
    reason: string;
    reference?: string;
  };
  status?: { phase?: string; expiresIn?: string };
}

export function Overrides() {
  const queryClient = useQueryClient();
  const toast = useToast();
  const { isOpen, onOpen, onClose } = useDisclosure();
  const [deleteTarget, setDeleteTarget] = useState<{ name: string; namespace: string } | null>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);

  const { data, isLoading, error } = useQuery<{ items: OverrideItem[]; count: number }>({
    queryKey: ['overrides'],
    queryFn: async () => { const r = await fetch('/api/v1/overrides'); if (!r.ok) throw new Error('Failed'); return r.json(); },
    refetchInterval: 10000,
  });

  const deleteMutation = useMutation({
    mutationFn: async ({ namespace, name }: { namespace: string; name: string }) => {
      const r = await fetch(`/api/v1/overrides/${namespace}/${name}`, { method: 'DELETE' });
      if (!r.ok) throw new Error('Failed to delete');
      return r.json();
    },
    onSuccess: () => {
      toast({ title: 'Override deleted', status: 'success', duration: 3000 });
      queryClient.invalidateQueries({ queryKey: ['overrides'] });
      onClose();
    },
    onError: () => { toast({ title: 'Failed to delete', status: 'error', duration: 3000 }); },
  });

  const handleDelete = (name: string, namespace: string) => { setDeleteTarget({ name, namespace }); onOpen(); };
  const confirmDelete = () => { if (deleteTarget) deleteMutation.mutate(deleteTarget); };

  if (isLoading) return <Spinner size="xl" />;
  if (error) return <Alert status="error"><AlertIcon />Failed to load overrides</Alert>;

  const overrides = data?.items ?? [];
  const active = overrides.filter(o => !o.status?.phase || o.status.phase === 'Active');
  const expired = overrides.filter(o => o.status?.phase === 'Expired');

  return (
    <VStack spacing={4} align="stretch">
      <HStack justify="space-between">
        <Heading size="lg">Overrides</Heading>
        <Text fontSize="sm" color="gray.500">{active.length} active, {expired.length} expired</Text>
      </HStack>

      {overrides.length === 0 ? (
        <Card>
          <CardBody textAlign="center" py={10}>
            <Text color="gray.500" mb={2}>No overrides exist</Text>
            <Text fontSize="sm" color="gray.400">Go to the Targets page and click "+ New Rule" → "Temporary Override" to create one</Text>
          </CardBody>
        </Card>
      ) : (
        <>
          {active.length > 0 && (
            <Box>
              <Text fontWeight="bold" fontSize="sm" mb={2} color="green.600">Active ({active.length})</Text>
              <OverrideTable overrides={active} onDelete={handleDelete} />
            </Box>
          )}
          {expired.length > 0 && (
            <Box>
              <Text fontWeight="bold" fontSize="sm" mb={2} color="gray.500">Expired ({expired.length})</Text>
              <OverrideTable overrides={expired} onDelete={handleDelete} />
            </Box>
          )}
        </>
      )}

      <AlertDialog isOpen={isOpen} leastDestructiveRef={cancelRef} onClose={onClose}>
        <AlertDialogOverlay>
          <AlertDialogContent>
            <AlertDialogHeader>Delete Override</AlertDialogHeader>
            <AlertDialogBody>Delete override <strong>{deleteTarget?.name}</strong>?</AlertDialogBody>
            <AlertDialogFooter>
              <Button ref={cancelRef} onClick={onClose}>Cancel</Button>
              <Button colorScheme="red" onClick={confirmDelete} ml={3} isLoading={deleteMutation.isPending}>Delete</Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialogOverlay>
      </AlertDialog>
    </VStack>
  );
}

function OverrideTable({ overrides, onDelete }: { overrides: OverrideItem[]; onDelete: (name: string, ns: string) => void }) {
  return (
    <Box overflowX="auto" bg="white" borderRadius="md" borderWidth={1}>
      <Table variant="simple" size="sm">
        <Thead bg="gray.50">
          <Tr>
            <Th>Target</Th>
            <Th>State</Th>
            <Th>Priority</Th>
            <Th>Expires</Th>
            <Th>Reason</Th>
            <Th>Ref</Th>
            <Th w="60px"></Th>
          </Tr>
        </Thead>
        <Tbody>
          {overrides.map(o => {
            const target = [...(o.spec.scope.namespaces || []), ...(o.spec.scope.workloadNames || [])].join('/');
            const isExpired = o.status?.phase === 'Expired' || new Date(o.spec.expiresAt) < new Date();
            return (
              <Tr key={o.metadata.name} opacity={isExpired ? 0.6 : 1} _hover={{ bg: 'gray.50' }}>
                <Td fontWeight="medium">{target || 'All'}</Td>
                <Td><Badge colorScheme={o.spec.state === 'on' ? 'green' : 'gray'}>{o.spec.state}</Badge></Td>
                <Td>{o.spec.priority}</Td>
                <Td>
                  <VStack align="start" spacing={0}>
                    <Text fontSize="sm">{new Date(o.spec.expiresAt).toLocaleString()}</Text>
                    {!isExpired && <Text fontSize="xs" color="orange.500">{o.status?.expiresIn || 'active'}</Text>}
                    {isExpired && <Text fontSize="xs" color="gray.400">expired</Text>}
                  </VStack>
                </Td>
                <Td><Text fontSize="sm" noOfLines={1} maxW="200px">{o.spec.reason}</Text></Td>
                <Td fontSize="xs" color="blue.500">{o.spec.reference || '—'}</Td>
                <Td>
                  <IconButton
                    aria-label="Delete"
                    icon={<Text>{'\u2715'}</Text>}
                    size="xs"
                    variant="ghost"
                    colorScheme="red"
                    onClick={() => onDelete(o.metadata.name, o.metadata.namespace)}
                  />
                </Td>
              </Tr>
            );
          })}
        </Tbody>
      </Table>
    </Box>
  );
}
