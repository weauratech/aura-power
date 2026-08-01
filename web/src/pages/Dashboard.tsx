import { SimpleGrid, Stat, StatLabel, StatNumber, StatHelpText, Card, CardBody, CardHeader, Heading, Spinner, Alert, AlertIcon, HStack, VStack, Text, Flex, Box } from '@chakra-ui/react';
import { PieChart, Pie, Cell, ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip, Legend } from 'recharts';
import { Link } from 'react-router-dom';
import { useStatus, useSavings, useTargets, usePolicies } from '../hooks/useApi';

const COLORS = { on: '#38A169', off: '#718096', blocked: '#E53E3E', divergent: '#DD6B20' };

export function Dashboard() {
  const { data: status, isLoading, error } = useStatus();
  const { data: savings } = useSavings();
  const { data: targetsData } = useTargets();
  const { data: policiesData } = usePolicies();

  if (isLoading) return <Flex justify="center" align="center" pt={20}><Spinner size="xl" color="blue.500" /><Text ml={3} color="gray.500">Loading dashboard...</Text></Flex>;
  if (error) return <Alert status="error" borderRadius="lg"><AlertIcon />Failed to connect to Aura Power controller</Alert>;
  if (!status) return null;

  const pieData = [
    { name: 'Powered On', value: status.poweredOn, color: COLORS.on },
    { name: 'Powered Off', value: status.poweredOff, color: COLORS.off },
    { name: 'Blocked', value: status.blocked, color: COLORS.blocked },
    { name: 'Divergent', value: status.divergent, color: COLORS.divergent },
  ].filter(d => d.value > 0);

  // Build namespace breakdown for bar chart
  const namespaceBreakdown = buildNamespaceBreakdown(targetsData?.targets ?? []);

  return (
    <VStack spacing={6} align="stretch">
      <Box>
        <Heading size="lg">Dashboard</Heading>
        <Text color="gray.500" fontSize="sm" mt={1}>Overview of your power management state</Text>
      </Box>

      {/* Summary Stats Row */}
      <SimpleGrid columns={{ base: 2, md: 3, lg: 6 }} spacing={4}>
        <StatCard label="Total Targets" value={status.totalTargets} color="blue.600" borderColor="blue.400" />
        <StatCard label="Powered On" value={status.poweredOn} color="green.500" helpText="active" borderColor="green.400" />
        <StatCard label="Powered Off" value={status.poweredOff} color="gray.600" helpText="saving costs" borderColor="gray.400" />
        <StatCard label="Blocked" value={status.blocked} color="red.500" helpText="guardrails" linkTo="/blocked" borderColor="red.400" />
        <StatCard label="Policies" value={status.activePolicies} color="purple.500" borderColor="purple.400" />
        <StatCard label="Overrides" value={status.activeOverrides} color="orange.500" borderColor="orange.400" />
      </SimpleGrid>

      {/* Charts Row */}
      <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={6}>
        {/* Pie Chart: Targets by State */}
        <Card shadow="sm" borderRadius="lg">
          <CardHeader pb={0}>
            <Heading size="sm" color="gray.700">Workloads by State</Heading>
          </CardHeader>
          <CardBody pt={4}>
            {pieData.length > 0 ? (
              <ResponsiveContainer width="100%" height={260}>
                <PieChart>
                  <Pie data={pieData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={90} label={({ name, value }) => `${name}: ${value}`}>
                    {pieData.map((entry, i) => (
                      <Cell key={i} fill={entry.color} />
                    ))}
                  </Pie>
                  <Tooltip />
                  <Legend />
                </PieChart>
              </ResponsiveContainer>
            ) : (
              <Flex justify="center" align="center" h={260} direction="column">
                <Text color="gray.400">No targets discovered yet</Text>
              </Flex>
            )}
          </CardBody>
        </Card>

        {/* Bar Chart: Targets per Namespace */}
        <Card shadow="sm" borderRadius="lg">
          <CardHeader pb={0}>
            <Heading size="sm" color="gray.700">Targets per Namespace</Heading>
          </CardHeader>
          <CardBody pt={4}>
            {namespaceBreakdown.length > 0 ? (
              <ResponsiveContainer width="100%" height={260}>
                <BarChart data={namespaceBreakdown} layout="vertical" margin={{ left: 80, right: 16, top: 8, bottom: 8 }}>
                  <XAxis type="number" />
                  <YAxis dataKey="namespace" type="category" width={80} tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Legend />
                  <Bar dataKey="on" stackId="a" fill={COLORS.on} name="On" />
                  <Bar dataKey="off" stackId="a" fill={COLORS.off} name="Off" />
                  <Bar dataKey="blocked" stackId="a" fill={COLORS.blocked} name="Blocked" />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <Flex justify="center" align="center" h={260} direction="column">
                <Text color="gray.400">No namespace data available</Text>
              </Flex>
            )}
          </CardBody>
        </Card>
      </SimpleGrid>

      {/* Savings Card — only show when there are actual savings */}
      {savings && savings.totalEstimatedCost > 0 && (
        <Card shadow="sm" borderRadius="lg" bg="green.50" borderColor="green.200" borderWidth={1}>
          <CardBody>
            <HStack justify="space-between" align="center">
              <VStack align="start" spacing={1}>
                <Heading size="sm" color="green.700">Estimated Savings</Heading>
                <Text fontSize="sm" color="green.600">Based on configured cost rates</Text>
              </VStack>
              <HStack spacing={8}>
                <VStack spacing={0}>
                  <Text fontSize="2xl" fontWeight="bold" color="green.700">
                    ${savings.totalEstimatedCost.toFixed(2)}
                  </Text>
                  <Text fontSize="xs" color="green.600">estimated cost saved</Text>
                </VStack>
                <VStack spacing={0}>
                  <Text fontSize="2xl" fontWeight="bold" color="green.700">
                    {savings.totalCPUHours.toFixed(0)}
                  </Text>
                  <Text fontSize="xs" color="green.600">CPU-hours saved</Text>
                </VStack>
              </HStack>
            </HStack>
          </CardBody>
        </Card>
      )}

      {/* Schedule Overview */}
      <Card shadow="sm" borderRadius="lg">
        <CardHeader pb={0}><HStack justify="space-between"><Heading size="sm" color="gray.700">Schedule Overview</Heading><Link to="/schedule"><Text fontSize="xs" color="blue.500" _hover={{ textDecoration: 'underline' }}>View full schedule</Text></Link></HStack></CardHeader>
        <CardBody pt={3}>
          {(policiesData?.items ?? []).length > 0 ? (
            <CompactScheduleGrid policies={policiesData?.items ?? []} />
          ) : (
            <Flex justify="center" align="center" h="80px"><Text fontSize="sm" color="gray.400">No active policies. Create a policy to see the schedule here.</Text></Flex>
          )}
        </CardBody>
      </Card>

      {/* Quick Status */}
      {status.divergent > 0 && (
        <Alert status="warning" borderRadius="lg" data-testid="divergent-alert">
          <AlertIcon />
          <Text color="gray.700">
            <Link to="/targets?state=divergent" style={{ textDecoration: 'underline', fontWeight: 'bold' }}>
              {status.divergent} workload{status.divergent > 1 ? 's' : ''}
            </Link>
            {' '}divergent — observed state differs from desired.
          </Text>
        </Alert>
      )}
    </VStack>
  );
}

function StatCard({ label, value, helpText, color, linkTo, borderColor }: { label: string; value: number; helpText?: string; color?: string; linkTo?: string; borderColor?: string }) {
  const card = (
    <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor={borderColor || 'transparent'} data-testid={`stat-${label.toLowerCase().replace(/\s/g, '-')}`}>
      <CardBody py={3} px={4}>
        <Stat>
          <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">{label}</StatLabel>
          <StatNumber fontSize="2xl" color={color}>{value}</StatNumber>
          {helpText && <StatHelpText fontSize="xs" mb={0} color="gray.500">{helpText}</StatHelpText>}
        </Stat>
      </CardBody>
    </Card>
  );
  if (linkTo) {
    return <Link to={linkTo} style={{ cursor: 'pointer' }}>{card}</Link>;
  }
  return card;
}

interface TargetItem {
  spec: { targetRef: { namespace: string } };
  status: { desiredState?: string; blocked?: boolean };
}

function buildNamespaceBreakdown(targets: TargetItem[]) {
  const nsMap: Record<string, { on: number; off: number; blocked: number }> = {};

  for (const t of targets) {
    const ns = t.spec.targetRef.namespace;
    if (!nsMap[ns]) nsMap[ns] = { on: 0, off: 0, blocked: 0 };

    if (t.status?.blocked) {
      nsMap[ns].blocked++;
    } else if (t.status?.desiredState === 'off') {
      nsMap[ns].off++;
    } else {
      nsMap[ns].on++;
    }
  }

  return Object.entries(nsMap)
    .map(([namespace, counts]) => ({ namespace, ...counts }))
    .sort((a, b) => (b.on + b.off + b.blocked) - (a.on + a.off + a.blocked))
    .slice(0, 10);
}


const DAYS_COMPACT = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
const HOURS_COMPACT = Array.from({ length: 24 }, (_, i) => i);
const DAY_MAP_COMPACT: Record<string, number> = { Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6, Sun: 0 };
const SCHEDULE_COLORS = ['blue', 'green', 'purple', 'orange', 'teal', 'pink', 'cyan'];

interface PolicyItem { metadata: { name: string }; spec: { schedule: { windows?: Array<{ start?: string; end?: string; days?: number[] }> } } }

function CompactScheduleGrid({ policies }: { policies: PolicyItem[] }) {
  const windows = policies.map((p, i) => {
    const w = p.spec.schedule.windows?.[0];
    const startHour = parseInt(w?.start?.split(':')[0] || '0');
    const endHour = parseInt(w?.end?.split(':')[0] || '24');
    const days = w?.days ?? [0, 1, 2, 3, 4, 5, 6];
    return { name: p.metadata.name, color: SCHEDULE_COLORS[i % SCHEDULE_COLORS.length], days, startHour, endHour };
  });

  const getCellPolicies = (dayNum: number, hour: number) => {
    return windows.filter(w => {
      if (!w.days.includes(dayNum)) return false;
      return w.startHour <= w.endHour ? hour >= w.startHour && hour < w.endHour : hour >= w.startHour || hour < w.endHour;
    });
  };

  return (
    <Box>
      {/* Legend */}
      <HStack spacing={3} mb={2} flexWrap="wrap">
        {windows.map(w => (
          <HStack key={w.name} spacing={1}>
            <Box w="10px" h="10px" bg={`${w.color}.400`} borderRadius="sm" />
            <Text fontSize="9px" color="gray.600">{w.name}</Text>
          </HStack>
        ))}
        <HStack spacing={1}>
          <Box w="10px" h="10px" bg="gray.100" border="1px solid" borderColor="gray.200" borderRadius="sm" />
          <Text fontSize="9px" color="gray.400">OFF</Text>
        </HStack>
      </HStack>
      <Flex>
        <Box w="35px" flexShrink={0} />
        {HOURS_COMPACT.filter((_, i) => i % 3 === 0).map(h => (
          <Box key={h} flex={3} textAlign="center"><Text fontSize="9px" color="gray.400">{h.toString().padStart(2, '0')}</Text></Box>
        ))}
      </Flex>
      {DAYS_COMPACT.map(day => {
        const dayNum = DAY_MAP_COMPACT[day];
        return (
          <Flex key={day} align="center" h="18px" mt="1px">
            <Box w="35px" flexShrink={0}><Text fontSize="9px" color="gray.500">{day}</Text></Box>
            {HOURS_COMPACT.map(hour => {
              const active = getCellPolicies(dayNum, hour);
              if (active.length === 0) {
                return <Box key={hour} flex={1} h="14px" bg="gray.50" borderWidth="1px" borderColor="gray.100" borderRadius="1px" />;
              }
              if (active.length === 1) {
                return <Box key={hour} flex={1} h="14px" bg={`${active[0].color}.100`} borderWidth="1px" borderColor={`${active[0].color}.200`} borderRadius="1px" />;
              }
              return (
                <Box key={hour} flex={1} h="14px" display="flex" flexDirection="column" borderWidth="1px" borderColor={`${active[0].color}.200`} borderRadius="1px" overflow="hidden">
                  <Box flex={1} bg={`${active[0].color}.200`} />
                  <Box flex={1} bg={`${active[1].color}.200`} />
                </Box>
              );
            })}
          </Flex>
        );
      })}
    </Box>
  );
}
