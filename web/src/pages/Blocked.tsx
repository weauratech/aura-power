import { Heading, VStack, Text, Badge, HStack, Spinner, Alert, AlertIcon, Box, Code, Flex, Table, Thead, Tbody, Tr, Th, Td, Button } from '@chakra-ui/react';
import { Link } from 'react-router-dom';
import { useTargets } from '../hooks/useApi';

export function Blocked() {
  const { data, isLoading, error } = useTargets();

  if (isLoading) return <Flex justify="center" align="center" py={20}><Spinner size="lg" color="blue.500" /><Text ml={3} color="gray.500">Loading...</Text></Flex>;
  if (error) return <Alert status="error" borderRadius="lg"><AlertIcon />Failed to load workloads</Alert>;

  const blockedTargets = (data?.targets ?? []).filter(t => t.status?.blocked);

  return (
    <VStack spacing={6} align="stretch">
      <Box>
        <Heading size="lg">Blocked Workloads</Heading>
        <Text color="gray.500" fontSize="sm" mt={1}>
          Workloads that cannot be managed by Aura Power due to guardrail violations
        </Text>
      </Box>

      {blockedTargets.length === 0 ? (
        <Box bg="white" borderRadius="lg" shadow="sm" p={12} textAlign="center">
          <Text color="gray.700" fontWeight="medium" mb={1}>No blocked workloads</Text>
          <Text fontSize="sm" color="gray.500" mb={4}>All discoverable workloads are eligible for power management</Text>
          <Link to="/targets">
            <Button size="sm" variant="ghost" colorScheme="blue">View all targets</Button>
          </Link>
        </Box>
      ) : (
        <Box bg="white" borderRadius="lg" shadow="sm" overflowX="auto">
          <Table size="sm">
            <Thead>
              <Tr>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Workload</Th>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Namespace</Th>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Kind</Th>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Block Reasons</Th>
                <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Waivable</Th>
              </Tr>
            </Thead>
            <Tbody>
              {blockedTargets.map(t => {
                const reasons = t.status?.blockReasons ?? [];
                const hasWaivable = reasons.some(r => r.waivable);
                return (
                  <Tr key={`${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`} _hover={{ bg: 'gray.50' }} transition="all 0.2s">
                    <Td>
                      <Link to={`/targets/${t.spec.targetRef.namespace}/${t.spec.targetRef.name}`}>
                        <Text fontWeight="medium" color="blue.600" _hover={{ textDecoration: 'underline' }} data-testid={`blocked-target-${t.spec.targetRef.name}`}>
                          {t.spec.targetRef.name}
                        </Text>
                      </Link>
                    </Td>
                    <Td fontSize="sm" color="gray.600">{t.spec.targetRef.namespace}</Td>
                    <Td><Badge variant="subtle" colorScheme="gray" fontSize="xs">{t.spec.targetRef.kind}</Badge></Td>
                    <Td>
                      <VStack align="start" spacing={1}>
                        {reasons.map((reason, i) => (
                          <HStack key={i} spacing={2}>
                            <Badge variant="subtle" colorScheme={reason.waivable ? 'orange' : 'red'} fontSize="xs">{reason.type}</Badge>
                            <Text fontSize="xs" color="gray.600" noOfLines={1}>{reason.message}</Text>
                          </HStack>
                        ))}
                        {reasons.length === 0 && <Text fontSize="xs" color="gray.400">No details</Text>}
                      </VStack>
                    </Td>
                    <Td>
                      {hasWaivable ? (
                        <Badge variant="subtle" colorScheme="orange" fontSize="xs">Yes</Badge>
                      ) : (
                        <Badge variant="subtle" colorScheme="red" fontSize="xs">No</Badge>
                      )}
                    </Td>
                  </Tr>
                );
              })}
            </Tbody>
          </Table>
          <Flex px={4} py={2} borderTopWidth={1} borderColor="gray.100">
            <Text fontSize="xs" color="gray.500">{blockedTargets.length} blocked workload{blockedTargets.length !== 1 ? 's' : ''}</Text>
          </Flex>
        </Box>
      )}

      {blockedTargets.length > 0 && (
        <Alert status="info" borderRadius="lg" data-testid="blocked-namespace-hint">
          <AlertIcon />
          <Box>
            <Text fontWeight="medium" fontSize="sm" color="gray.700">How to unblock waivable workloads</Text>
            <Text fontSize="sm" color="gray.600">
              Add annotation <Code fontSize="xs">aura.sh/power-eligible=true</Code> to the workload or namespace to allow power management despite external ownership.
            </Text>
          </Box>
        </Alert>
      )}
    </VStack>
  );
}
