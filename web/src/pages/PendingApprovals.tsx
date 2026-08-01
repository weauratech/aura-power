import { Box, Heading, Table, Thead, Tbody, Tr, Th, Td, Badge, Spinner, Alert, AlertIcon, Button, HStack, VStack, Text, Flex, Card, CardBody, useToast } from '@chakra-ui/react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';


interface PendingChange {
  id: string;
  username: string;
  action: string;
  resourceKind: string;
  resourceName: string;
  status: string;
  createdAt: string;
}

export function PendingApprovals() {
  const toast = useToast();
  const queryClient = useQueryClient();

  const { data, isLoading, error } = useQuery<{ items: PendingChange[]; count: number }>({
    queryKey: ['pending'],
    queryFn: async () => {
      const res = await fetch('/api/v1/pending', { credentials: 'same-origin' as RequestCredentials });
      if (res.status === 403) throw new Error('insufficient permissions');
      if (!res.ok) throw new Error('Failed to load');
      return res.json();
    },
    refetchInterval: 10000,
  });

  const approveMutation = useMutation({
    mutationFn: async (id: string) => {
      const res = await fetch(`/api/v1/pending/${id}/approve`, { method: 'POST', credentials: 'same-origin' as RequestCredentials });
      if (!res.ok) throw new Error('Failed');
    },
    onSuccess: () => { toast({ title: 'Approved', status: 'success', duration: 2000 }); queryClient.invalidateQueries({ queryKey: ['pending'] }); },
  });

  const rejectMutation = useMutation({
    mutationFn: async (id: string) => {
      const res = await fetch(`/api/v1/pending/${id}/reject`, { method: 'POST', credentials: 'same-origin' as RequestCredentials });
      if (!res.ok) throw new Error('Failed');
    },
    onSuccess: () => { toast({ title: 'Rejected', status: 'info', duration: 2000 }); queryClient.invalidateQueries({ queryKey: ['pending'] }); },
  });

  if (isLoading) return <Flex justify="center" py={12}><Spinner size="lg" color="blue.500" /><Text ml={3} color="gray.500">Loading...</Text></Flex>;

  if (error?.message === 'insufficient permissions') {
    return <Alert status="warning" borderRadius="lg"><AlertIcon />You need Approver or Admin role to view pending changes.</Alert>;
  }
  if (error) return <Alert status="error" borderRadius="lg"><AlertIcon />Failed to load pending changes</Alert>;

  const items = data?.items ?? [];

  return (
    <VStack spacing={6} align="stretch">
      <Box>
        <Heading size="lg">Pending Approvals</Heading>
        <Text color="gray.500" fontSize="sm" mt={1}>Changes submitted by members awaiting approval</Text>
      </Box>

      {items.length === 0 ? (
        <Card shadow="sm" borderRadius="lg">
          <CardBody textAlign="center" py={12}>
            <Text color="gray.700" fontWeight="medium" mb={1}>No pending changes</Text>
            <Text fontSize="sm" color="gray.500">All submitted changes have been reviewed</Text>
          </CardBody>
        </Card>
      ) : (
        <Box bg="white" borderRadius="lg" shadow="sm" overflowX="auto">
          <Table size="sm">
            <Thead>
              <Tr>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500">User</Th>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500">Action</Th>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500">Resource</Th>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500">Name</Th>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500">Submitted</Th>
                <Th w="160px"></Th>
              </Tr>
            </Thead>
            <Tbody>
              {items.map(item => (
                <Tr key={item.id} _hover={{ bg: 'gray.50' }}>
                  <Td fontWeight="medium" color="gray.700">{item.username}</Td>
                  <Td><Badge variant="subtle" colorScheme={item.action === 'delete' ? 'red' : item.action === 'create' ? 'green' : 'blue'} fontSize="xs">{item.action}</Badge></Td>
                  <Td fontSize="sm" color="gray.600">{item.resourceKind}</Td>
                  <Td fontSize="sm" color="gray.700">{item.resourceName}</Td>
                  <Td fontSize="xs" color="gray.500">{new Date(item.createdAt).toLocaleString()}</Td>
                  <Td>
                    <HStack spacing={2}>
                      <Button size="xs" colorScheme="green" onClick={() => approveMutation.mutate(item.id)} isLoading={approveMutation.isPending} data-testid={`approve-${item.id}`}>
                        Approve
                      </Button>
                      <Button size="xs" variant="outline" colorScheme="red" onClick={() => rejectMutation.mutate(item.id)} isLoading={rejectMutation.isPending} data-testid={`reject-${item.id}`}>
                        Reject
                      </Button>
                    </HStack>
                  </Td>
                </Tr>
              ))}
            </Tbody>
          </Table>
        </Box>
      )}
    </VStack>
  );
}
