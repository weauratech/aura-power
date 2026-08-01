import { Box, Heading, Card, CardBody, CardHeader, SimpleGrid, Spinner, Alert, AlertIcon, VStack, Text, HStack, Button, Flex, Stat, StatLabel, StatNumber, Select } from '@chakra-ui/react';
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, Legend, CartesianGrid, ComposedChart, Bar } from 'recharts';
import { useState } from 'react';
import { useClusterMetrics, useNodeMetrics, useNamespaceMetrics, useCostSummary } from '../hooks/useMetrics';
import { useNamespaces } from '../hooks/useApi';

const RANGES = ['1h', '6h', '24h', '7d'] as const;

export function Metrics() {
  const [range, setRange] = useState<string>('24h');
  const [selectedNs, setSelectedNs] = useState<string>('');
  const { data: cluster, isLoading: loadingCluster, error: clusterError } = useClusterMetrics(range);
  const { data: nodes, isLoading: loadingNodes } = useNodeMetrics(range);
  const { data: nsData } = useNamespaces();
  const { data: nsMetrics } = useNamespaceMetrics(selectedNs, range);
  const { data: costData } = useCostSummary();

  const unavailable = clusterError?.message?.includes('not available');

  if (unavailable) {
    return (
      <VStack spacing={6} align="stretch">
        <Box>
          <Heading size="lg">Metrics</Heading>
          <Text color="gray.500" fontSize="sm" mt={1}>Real-time resource usage and cost visibility</Text>
        </Box>
        <Alert status="warning" borderRadius="lg">
          <AlertIcon />
          <Box>
            <Text fontWeight="medium">Metrics provider not connected</Text>
            <Text fontSize="sm" color="gray.600">
              Configure Prometheus or metrics-server in the Helm values to enable resource metrics.
              See the documentation for setup instructions.
            </Text>
          </Box>
        </Alert>
      </VStack>
    );
  }

  const isLoading = loadingCluster || loadingNodes;

  // Transform data for charts
  const cpuChartData = buildTripleChartData(cluster?.cpuUsage, cluster?.cpuCapacity, cluster?.cpuRequested, 'Usage', 'Capacity', 'Requested');
  const memChartData = buildTripleChartData(
    cluster?.memoryUsage?.map(s => ({ ...s, value: s.value / (1024 * 1024 * 1024) })),
    cluster?.memoryCapacity?.map(s => ({ ...s, value: s.value / (1024 * 1024 * 1024) })),
    cluster?.memoryRequested?.map(s => ({ ...s, value: s.value / (1024 * 1024 * 1024) })),
    'Usage', 'Capacity', 'Requested'
  );
  const nodeChartData = buildNodeChartData(nodes?.nodeCount);

  // Current values for stats
  const currentCPU = cluster?.cpuUsage?.length ? cluster.cpuUsage[cluster.cpuUsage.length - 1].value : 0;
  const cpuCap = cluster?.cpuCapacity?.length ? cluster.cpuCapacity[cluster.cpuCapacity.length - 1].value : 1;
  const currentMem = cluster?.memoryUsage?.length ? cluster.memoryUsage[cluster.memoryUsage.length - 1].value / (1024 * 1024 * 1024) : 0;
  const memCap = cluster?.memoryCapacity?.length ? cluster.memoryCapacity[cluster.memoryCapacity.length - 1].value / (1024 * 1024 * 1024) : 1;
  const currentNodes = nodes?.nodeCount?.length ? nodes.nodeCount[nodes.nodeCount.length - 1].value : 0;

  return (
    <VStack spacing={6} align="stretch">
      <HStack justify="space-between">
        <Box>
          <Heading size="lg">Metrics</Heading>
          <Text color="gray.500" fontSize="sm" mt={1}>Real-time resource usage and cost visibility</Text>
        </Box>
        {/* Time Range Selector */}
        <HStack spacing={1} bg="white" p={1} borderRadius="md" shadow="sm">
          {RANGES.map(r => (
            <Button
              key={r}
              size="sm"
              variant={range === r ? 'solid' : 'ghost'}
              colorScheme={range === r ? 'blue' : 'gray'}
              onClick={() => setRange(r)}
              data-testid={`range-${r}`}
            >
              {r}
            </Button>
          ))}
        </HStack>
      </HStack>

      {/* Stats Row */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="blue.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase">CPU Usage</StatLabel>
              <StatNumber fontSize="xl" color="blue.600">{((currentCPU / cpuCap) * 100).toFixed(0)}%</StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="purple.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase">Memory Usage</StatLabel>
              <StatNumber fontSize="xl" color="purple.600">{((currentMem / memCap) * 100).toFixed(0)}%</StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="green.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase">Nodes</StatLabel>
              <StatNumber fontSize="xl" color="green.600">{currentNodes}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="orange.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase">Cost / day</StatLabel>
              <StatNumber fontSize="xl" color="orange.600">
                {costData?.totalClusterCostPerHour ? `$${(costData.totalClusterCostPerHour * 24).toFixed(2)}` : 'N/A'}
              </StatNumber>
            </Stat>
          </CardBody>
        </Card>
      </SimpleGrid>

      {isLoading ? (
        <Flex justify="center" py={12}><Spinner size="lg" color="blue.500" /><Text ml={3} color="gray.500">Loading metrics...</Text></Flex>
      ) : (
        <>
          {/* Two charts side by side */}
          <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={6}>
            <Card shadow="sm" borderRadius="lg">
              <CardHeader pb={0}><Heading size="sm" color="gray.700">CPU Usage (cores)</Heading></CardHeader>
              <CardBody>
                <ResponsiveContainer width="100%" height={240}>
                  <AreaChart data={cpuChartData} margin={{ top: 10, right: 10, bottom: 0, left: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#E2E8F0" />
                    <XAxis dataKey="time" tick={{ fontSize: 10 }} stroke="#A0AEC0" />
                    <YAxis tick={{ fontSize: 10 }} stroke="#A0AEC0" />
                    <Tooltip />
                    <Legend />
                    <Area type="monotone" dataKey="Capacity" stroke="#CBD5E0" fill="#EDF2F7" strokeDasharray="4 4" />
                    <Area type="monotone" dataKey="Requested" stroke="#E53E3E" fill="none" strokeDasharray="2 2" strokeWidth={1.5} />
                    <Area type="monotone" dataKey="Usage" stroke="#3182CE" fill="#BEE3F8" fillOpacity={0.6} />
                  </AreaChart>
                </ResponsiveContainer>
              </CardBody>
            </Card>

            <Card shadow="sm" borderRadius="lg">
              <CardHeader pb={0}><Heading size="sm" color="gray.700">Memory Usage (GiB)</Heading></CardHeader>
              <CardBody>
                <ResponsiveContainer width="100%" height={240}>
                  <AreaChart data={memChartData} margin={{ top: 10, right: 10, bottom: 0, left: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#E2E8F0" />
                    <XAxis dataKey="time" tick={{ fontSize: 10 }} stroke="#A0AEC0" />
                    <YAxis tick={{ fontSize: 10 }} stroke="#A0AEC0" />
                    <Tooltip />
                    <Legend />
                    <Area type="monotone" dataKey="Capacity" stroke="#CBD5E0" fill="#EDF2F7" strokeDasharray="4 4" />
                    <Area type="monotone" dataKey="Requested" stroke="#E53E3E" fill="none" strokeDasharray="2 2" strokeWidth={1.5} />
                    <Area type="monotone" dataKey="Usage" stroke="#805AD5" fill="#E9D8FD" fillOpacity={0.6} />
                  </AreaChart>
                </ResponsiveContainer>
              </CardBody>
            </Card>
          </SimpleGrid>

          {/* Full-width chart: Nodes */}
          <Card shadow="sm" borderRadius="lg">
            <CardHeader pb={0}><Heading size="sm" color="gray.700">Nodes</Heading></CardHeader>
            <CardBody>
              <ResponsiveContainer width="100%" height={280}>
                <ComposedChart data={nodeChartData} margin={{ top: 10, right: 10, bottom: 0, left: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#E2E8F0" />
                  <XAxis dataKey="time" tick={{ fontSize: 10 }} stroke="#A0AEC0" />
                  <YAxis tick={{ fontSize: 10 }} stroke="#A0AEC0" />
                  <Tooltip />
                  <Legend />
                  <Bar dataKey="nodes" fill="#38A169" name="Node Count" opacity={0.7} />
                </ComposedChart>
              </ResponsiveContainer>
            </CardBody>
          </Card>
        </>
      )}

      {/* Namespace Drill-down */}
      <Card shadow="sm" borderRadius="lg">
        <CardHeader pb={0}>
          <HStack justify="space-between">
            <Heading size="sm" color="gray.700">Namespace Drill-down</Heading>
            <Select maxW="220px" size="sm" value={selectedNs} onChange={e => setSelectedNs(e.target.value)} placeholder="Select namespace..." bg="white">
              {(nsData?.namespaces ?? []).filter(ns => !['kube-system', 'kube-public', 'kube-node-lease'].includes(ns)).sort().map(ns => (
                <option key={ns} value={ns}>{ns}</option>
              ))}
            </Select>
          </HStack>
        </CardHeader>
        <CardBody>
          {!selectedNs ? (
            <Flex justify="center" align="center" h={200}><Text color="gray.400">Select a namespace to view resource usage</Text></Flex>
          ) : nsMetrics ? (
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <Box>
                <Text fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>CPU Usage</Text>
                <ResponsiveContainer width="100%" height={180}>
                  <AreaChart data={buildChartData(nsMetrics.cpuUsage, nsMetrics.cpuRequested, 'Actual', 'Requested')} margin={{ top: 5, right: 5, bottom: 0, left: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#E2E8F0" />
                    <XAxis dataKey="time" tick={{ fontSize: 9 }} stroke="#A0AEC0" />
                    <YAxis tick={{ fontSize: 9 }} stroke="#A0AEC0" />
                    <Tooltip />
                    <Area type="monotone" dataKey="Requested" stroke="#CBD5E0" fill="#EDF2F7" strokeDasharray="4 4" />
                    <Area type="monotone" dataKey="Actual" stroke="#3182CE" fill="#BEE3F8" fillOpacity={0.6} />
                  </AreaChart>
                </ResponsiveContainer>
              </Box>
              <Box>
                <Text fontSize="xs" fontWeight="bold" color="gray.500" mb={2}>Memory Usage (GiB)</Text>
                <ResponsiveContainer width="100%" height={180}>
                  <AreaChart data={buildChartData(nsMetrics.memoryUsage?.map(s => ({ ...s, value: s.value / (1024 * 1024 * 1024) })), nsMetrics.memoryRequested?.map(s => ({ ...s, value: s.value / (1024 * 1024 * 1024) })), 'Actual', 'Requested')} margin={{ top: 5, right: 5, bottom: 0, left: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#E2E8F0" />
                    <XAxis dataKey="time" tick={{ fontSize: 9 }} stroke="#A0AEC0" />
                    <YAxis tick={{ fontSize: 9 }} stroke="#A0AEC0" />
                    <Tooltip />
                    <Area type="monotone" dataKey="Requested" stroke="#CBD5E0" fill="#EDF2F7" strokeDasharray="4 4" />
                    <Area type="monotone" dataKey="Actual" stroke="#805AD5" fill="#E9D8FD" fillOpacity={0.6} />
                  </AreaChart>
                </ResponsiveContainer>
              </Box>
            </SimpleGrid>
          ) : (
            <Flex justify="center" align="center" h={200}><Spinner size="sm" /><Text ml={2} color="gray.500" fontSize="sm">Loading namespace metrics...</Text></Flex>
          )}
        </CardBody>
      </Card>
    </VStack>
  );
}

interface Sample { timestamp: string; value: number }

function buildChartData(usage?: Sample[], capacity?: Sample[], usageLabel?: string, capLabel?: string) {
  if (!usage || usage.length === 0) return [];
  return usage.map((s, i) => ({
    time: new Date(s.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    [usageLabel || 'Usage']: Number(s.value.toFixed(2)),
    [capLabel || 'Capacity']: capacity?.[i] ? Number(capacity[i].value.toFixed(2)) : undefined,
  }));
}

function buildNodeChartData(nodeCount?: Sample[]) {
  if (!nodeCount || nodeCount.length === 0) return [];
  return nodeCount.map(s => ({
    time: new Date(s.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    nodes: s.value,
    costPerHour: 0, // Will be populated when OpenCost data available
  }));
}


function buildTripleChartData(usage?: Sample[], capacity?: Sample[], requested?: Sample[], usageLabel?: string, capLabel?: string, reqLabel?: string) {
  if (!usage || usage.length === 0) return [];
  return usage.map((s, i) => ({
    time: new Date(s.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    [usageLabel || 'Usage']: Number(s.value.toFixed(2)),
    [capLabel || 'Capacity']: capacity?.[i] ? Number(capacity[i].value.toFixed(2)) : undefined,
    [reqLabel || 'Requested']: requested?.[i] ? Number(requested[i].value.toFixed(2)) : undefined,
  }));
}
