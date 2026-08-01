import {
  Heading, SimpleGrid, Card, CardBody, Stat, StatLabel, StatNumber, StatHelpText,
  Spinner, VStack, Text, Box, Flex, Button, Badge, Table, Thead, Tbody, Tr, Th, Td,
  Progress, Accordion, AccordionItem, AccordionButton, AccordionPanel, AccordionIcon
} from '@chakra-ui/react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useSavings, useStatus } from '../hooks/useApi';
import { useCostSummary } from '../hooks/useMetrics';

interface WorkloadSaving {
  namespace: string;
  name: string;
  kind: string;
  cpuHours: number;
  memoryGiBHours: number;
  estimatedCost: number;
  desiredState: string;
}

interface NamespaceSaving {
  namespace: string;
  cost: number;
}

interface BreakdownData {
  workloads: WorkloadSaving[] | null;
  byNamespace: NamespaceSaving[] | null;
  poweredOff: number;
  totalTargets: number;
}

function useSavingsBreakdown() {
  return useQuery<BreakdownData>({
    queryKey: ['savings-breakdown'],
    queryFn: async () => {
      const res = await fetch('/api/v1/savings/breakdown', { credentials: 'same-origin' });
      if (!res.ok) throw new Error('Failed to load');
      return res.json();
    },
    refetchInterval: 30000,
  });
}

export function Savings() {
  const { data: summary, isLoading: summaryLoading } = useSavings();
  const { data: breakdown, isLoading: breakdownLoading } = useSavingsBreakdown();
  const { data: status } = useStatus();
  const { data: costData } = useCostSummary();

  const isLoading = summaryLoading || breakdownLoading;

  if (isLoading) {
    return (
      <Flex justify="center" align="center" py={20}>
        <Spinner size="lg" color="blue.500" />
        <Text ml={3} color="gray.500">Loading savings data...</Text>
      </Flex>
    );
  }

  const totalCPU = summary?.totalCPUHours ?? 0;
  const totalMem = summary?.totalMemoryGiB ?? 0;
  const totalCost = summary?.totalEstimatedCost ?? 0;
  const poweredOff = breakdown?.poweredOff ?? status?.poweredOff ?? 0;
  const totalTargets = breakdown?.totalTargets ?? status?.totalTargets ?? 0;
  const workloads = breakdown?.workloads ?? [];
  const byNamespace = (breakdown?.byNamespace ?? []).sort((a, b) => b.cost - a.cost);
  const hasSavings = totalCPU > 0 || totalMem > 0 || totalCost > 0;

  return (
    <VStack spacing={6} align="stretch">
      <Box>
        <Heading size="lg">Savings</Heading>
        <Text color="gray.500" fontSize="sm" mt={1}>
          Resource and cost savings from workloads managed by Aura Power
        </Text>
      </Box>

      {/* Summary Cards */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Card shadow="sm" borderRadius="lg">
          <CardBody py={4} px={5}>
            <Stat size="sm">
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">Cost Saved</StatLabel>
              <StatNumber fontSize="2xl" color="green.600">${totalCost.toFixed(2)}</StatNumber>
              <StatHelpText fontSize="xs" color="gray.400" mb={0}>accumulated total</StatHelpText>
            </Stat>
          </CardBody>
        </Card>

        <Card shadow="sm" borderRadius="lg">
          <CardBody py={4} px={5}>
            <Stat size="sm">
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">CPU Hours Saved</StatLabel>
              <StatNumber fontSize="2xl" color="blue.600">{totalCPU.toFixed(1)}</StatNumber>
              <StatHelpText fontSize="xs" color="gray.400" mb={0}>compute-hours freed</StatHelpText>
            </Stat>
          </CardBody>
        </Card>

        <Card shadow="sm" borderRadius="lg">
          <CardBody py={4} px={5}>
            <Stat size="sm">
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">Memory Saved</StatLabel>
              <StatNumber fontSize="2xl" color="purple.600">{totalMem.toFixed(1)}</StatNumber>
              <StatHelpText fontSize="xs" color="gray.400" mb={0}>GiB-hours freed</StatHelpText>
            </Stat>
          </CardBody>
        </Card>

        <Card shadow="sm" borderRadius="lg">
          <CardBody py={4} px={5}>
            <Stat size="sm">
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">Powered Off</StatLabel>
              <StatNumber fontSize="2xl" color="orange.500">{poweredOff}</StatNumber>
              <StatHelpText fontSize="xs" color="gray.400" mb={0}>of {totalTargets} workloads</StatHelpText>
            </Stat>
          </CardBody>
        </Card>
      </SimpleGrid>

      {!hasSavings && poweredOff === 0 && (
        <Card shadow="sm" borderRadius="lg">
          <CardBody textAlign="center" py={10}>
            <Text fontWeight="medium" color="gray.600" mb={2}>No savings accumulated yet</Text>
            <Text fontSize="sm" color="gray.400" mb={4}>
              Create a power policy to start scheduling workload shutdowns and accumulating savings.
            </Text>
            {costData && costData.totalClusterCostPerHour > 0 && (
              <Box bg="gray.50" borderRadius="md" p={4} mb={4} maxW="400px" mx="auto">
                <Text fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide" mb={2}>Current cluster cost (via OpenCost)</Text>
                <SimpleGrid columns={2} spacing={3}>
                  <Box>
                    <Text fontSize="xl" fontWeight="bold" color="gray.700">${(costData.totalClusterCostPerHour * 24).toFixed(2)}</Text>
                    <Text fontSize="xs" color="gray.500">per day</Text>
                  </Box>
                  <Box>
                    <Text fontSize="xl" fontWeight="bold" color="gray.700">${(costData.totalClusterCostPerHour * 730).toFixed(0)}</Text>
                    <Text fontSize="xs" color="gray.500">projected / month</Text>
                  </Box>
                </SimpleGrid>
                <Text fontSize="xs" color="gray.400" mt={3}>
                  Power off non-essential workloads during off-hours to reduce these costs.
                </Text>
              </Box>
            )}
            <Link to="/rules">
              <Button colorScheme="blue" size="sm">Create a policy</Button>
            </Link>
          </CardBody>
        </Card>
      )}

      {/* Savings by Namespace */}
      {byNamespace.length > 0 && (
        <Card shadow="sm" borderRadius="lg">
          <CardBody>
            <Heading size="sm" mb={4} color="gray.700">Savings by Namespace</Heading>
            <VStack spacing={3} align="stretch">
              {byNamespace.slice(0, 10).map((ns) => {
                const maxCost = byNamespace[0]?.cost ?? 1;
                const pct = maxCost > 0 ? (ns.cost / maxCost) * 100 : 0;
                return (
                  <Box key={ns.namespace}>
                    <Flex justify="space-between" mb={1}>
                      <Text fontSize="sm" fontWeight="medium" color="gray.700">{ns.namespace}</Text>
                      <Text fontSize="sm" fontWeight="semibold" color="green.600">${ns.cost.toFixed(2)}</Text>
                    </Flex>
                    <Progress value={pct} size="sm" colorScheme="green" borderRadius="full" />
                  </Box>
                );
              })}
            </VStack>
          </CardBody>
        </Card>
      )}

      {/* Workload Breakdown Table */}
      {workloads.length > 0 && (
        <Card shadow="sm" borderRadius="lg">
          <CardBody p={0}>
            <Box px={5} pt={4} pb={2}>
              <Heading size="sm" color="gray.700">Workload Breakdown</Heading>
              <Text fontSize="xs" color="gray.400" mt={1}>Individual savings per workload</Text>
            </Box>
            <Box overflowX="auto">
              <Table size="sm">
                <Thead>
                  <Tr>
                    <Th fontSize="xs">Namespace</Th>
                    <Th fontSize="xs">Workload</Th>
                    <Th fontSize="xs">Kind</Th>
                    <Th fontSize="xs" isNumeric>CPU Hours</Th>
                    <Th fontSize="xs" isNumeric>Memory GiB-h</Th>
                    <Th fontSize="xs" isNumeric>Est. Cost</Th>
                    <Th fontSize="xs">State</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {workloads
                    .sort((a, b) => b.estimatedCost - a.estimatedCost)
                    .slice(0, 20)
                    .map((w) => (
                      <Tr key={`${w.namespace}/${w.name}`} _hover={{ bg: 'gray.50' }}>
                        <Td fontSize="sm" color="gray.600">{w.namespace}</Td>
                        <Td fontSize="sm" fontWeight="medium">{w.name}</Td>
                        <Td><Badge size="sm" colorScheme="gray" fontSize="xs">{w.kind}</Badge></Td>
                        <Td fontSize="sm" isNumeric>{w.cpuHours.toFixed(1)}</Td>
                        <Td fontSize="sm" isNumeric>{w.memoryGiBHours.toFixed(1)}</Td>
                        <Td fontSize="sm" isNumeric fontWeight="semibold" color="green.600">${w.estimatedCost.toFixed(2)}</Td>
                        <Td><Badge colorScheme={w.desiredState === 'off' ? 'red' : 'green'} fontSize="xs">{w.desiredState}</Badge></Td>
                      </Tr>
                    ))}
                </Tbody>
              </Table>
            </Box>
          </CardBody>
        </Card>
      )}

      {/* How it works */}
      <Card shadow="sm" borderRadius="lg" variant="outline">
        <CardBody p={0}>
          <Accordion allowToggle>
            <AccordionItem border="none">
              <AccordionButton px={5} py={4}>
                <Box flex="1" textAlign="left">
                  <Text fontSize="sm" fontWeight="medium" color="gray.700">How savings are calculated</Text>
                </Box>
                <AccordionIcon />
              </AccordionButton>
              <AccordionPanel px={5} pb={4}>
                <VStack spacing={3} align="stretch">
                  <Text fontSize="sm" color="gray.600">
                    Savings are calculated based on the resources (CPU and memory) that each workload
                    was consuming before being powered off, multiplied by the time it has been off.
                  </Text>
                  <Box bg="gray.50" p={3} borderRadius="md">
                    <Text fontSize="xs" fontWeight="medium" color="gray.600" mb={2}>Formula:</Text>
                    <Text fontSize="xs" color="gray.500" fontFamily="mono">
                      cost = (vCPU x $0.032/h) + (GiB x $0.004/h) x hours_off
                    </Text>
                  </Box>
                  <SimpleGrid columns={2} spacing={3}>
                    <Box>
                      <Text fontSize="xs" fontWeight="medium" color="gray.600">CPU Rate</Text>
                      <Text fontSize="sm" color="gray.700">$0.032 / vCPU-hour</Text>
                    </Box>
                    <Box>
                      <Text fontSize="xs" fontWeight="medium" color="gray.600">Memory Rate</Text>
                      <Text fontSize="sm" color="gray.700">$0.004 / GiB-hour</Text>
                    </Box>
                  </SimpleGrid>
                  <Text fontSize="xs" color="gray.400">
                    Rates reflect on-demand equivalent pricing. Configure custom rates via the controller Helm values.
                    
                  </Text>
                </VStack>
              </AccordionPanel>
            </AccordionItem>
          </Accordion>
        </CardBody>
      </Card>
    </VStack>
  );
}
