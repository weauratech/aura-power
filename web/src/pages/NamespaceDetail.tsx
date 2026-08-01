import { Box, Heading, Table, Thead, Tbody, Tr, Th, Td, Badge, Spinner, Alert, AlertIcon, VStack, Text, Flex, HStack, Card, CardBody, Stat, StatLabel, StatNumber, SimpleGrid } from '@chakra-ui/react';
import { useParams, Link } from 'react-router-dom';
import { useTargets } from '../hooks/useApi';
import { StatusBadge } from '../components/StatusBadge';

export function NamespaceDetail() {
  const { namespace } = useParams<{ namespace: string }>();
  const { data, isLoading, error } = useTargets(namespace);

  if (isLoading) return <Flex justify="center" align="center" py={20}><Spinner size="lg" color="blue.500" /><Text ml={3} color="gray.500">Loading...</Text></Flex>;
  if (error) return <Alert status="error" borderRadius="lg"><AlertIcon />Failed to load targets</Alert>;

  const targets = data?.targets ?? [];
  const onCount = targets.filter(t => !t.status?.blocked && t.status?.desiredState !== 'off').length;
  const offCount = targets.filter(t => t.status?.desiredState === 'off' && !t.status?.blocked).length;
  const blockedCount = targets.filter(t => t.status?.blocked).length;

  return (
    <VStack spacing={6} align="stretch">
      <Box>
        <HStack mb={1}>
          <Link to="/targets"><Text fontSize="sm" color="blue.500" _hover={{ textDecoration: 'underline' }}>Targets</Text></Link>
          <Text fontSize="sm" color="gray.400">/</Text>
          <Text fontSize="sm" color="gray.600">{namespace}</Text>
        </HStack>
        <Heading size="lg">{namespace}</Heading>
        <Text color="gray.500" fontSize="sm" mt={1}>{targets.length} workloads in this namespace</Text>
      </Box>

      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="blue.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase">Total</StatLabel>
              <StatNumber fontSize="xl" color="blue.600">{targets.length}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="green.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase">On</StatLabel>
              <StatNumber fontSize="xl" color="green.500">{onCount}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="gray.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase">Off</StatLabel>
              <StatNumber fontSize="xl" color="gray.600">{offCount}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="red.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase">Blocked</StatLabel>
              <StatNumber fontSize="xl" color="red.500">{blockedCount}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
      </SimpleGrid>

      <Box bg="white" borderRadius="lg" shadow="sm" overflowX="auto">
        <Table size="sm">
          <Thead>
            <Tr>
              <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Workload</Th>
              <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Kind</Th>
              <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Desired</Th>
              <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Observed</Th>
              <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Status</Th>
              <Th fontSize="xs" textTransform="uppercase" letterSpacing="wider" color="gray.500" fontWeight="bold">Rule</Th>
            </Tr>
          </Thead>
          <Tbody>
            {targets.map(t => (
              <Tr key={t.spec.targetRef.name} _hover={{ bg: 'gray.50' }} transition="all 0.2s">
                <Td>
                  <Link to={`/targets/${namespace}/${t.spec.targetRef.name}`}>
                    <Text fontWeight="medium" color="blue.600" cursor="pointer" _hover={{ textDecoration: 'underline' }}>{t.spec.targetRef.name}</Text>
                  </Link>
                </Td>
                <Td><Badge variant="subtle" colorScheme="gray" fontSize="xs">{t.spec.targetRef.kind}</Badge></Td>
                <Td><StatusBadge state={t.status?.desiredState || 'unmanaged'} /></Td>
                <Td><StatusBadge state={t.status?.observedState?.powerState || 'unknown'} /></Td>
                <Td>
                  {t.status?.blocked && <Badge variant="subtle" colorScheme="red" fontSize="xs">Blocked</Badge>}
                  {t.status?.divergent && <Badge variant="subtle" colorScheme="orange" fontSize="xs">Divergent</Badge>}
                  {!t.status?.blocked && !t.status?.divergent && t.status?.managed && <Badge variant="subtle" colorScheme="green" fontSize="xs">Managed</Badge>}
                  {!t.status?.managed && <Badge variant="subtle" colorScheme="gray" fontSize="xs">Unmanaged</Badge>}
                </Td>
                <Td fontSize="xs" color="gray.500">{t.status?.winningRule?.name || '-'}</Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      </Box>
    </VStack>
  );
}
