import { Box, Heading, VStack, Text, HStack, Card, CardBody, Flex, Spinner, Select, Tooltip as ChakraTooltip, useDisclosure } from '@chakra-ui/react';
import { usePolicies, PolicyResponse } from '../hooks/useApi';
import { useState } from 'react';
import { CreateRuleDrawer } from '../components/CreateRuleDrawer';

const DAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
const HOURS = Array.from({ length: 24 }, (_, i) => i);
const DAY_MAP: Record<string, number> = { Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6, Sun: 0 };
const POLICY_COLORS = ['blue', 'green', 'purple', 'orange', 'teal', 'pink', 'cyan', 'red'];

interface ParsedWindow {
  name: string;
  color: string;
  days: number[];
  startHour: number;
  endHour: number;
  policy: PolicyResponse;
}

export function Schedule() {
  const { data, isLoading } = usePolicies();
  const [selectedPolicy, setSelectedPolicy] = useState<string>('all');
  const { isOpen, onOpen, onClose } = useDisclosure();
  const [editPolicy, setEditPolicy] = useState<PolicyResponse | null>(null);

  if (isLoading) return <Flex justify="center" py={12}><Spinner size="lg" color="blue.500" /><Text ml={3} color="gray.500">Loading...</Text></Flex>;

  const policies = data?.items ?? [];

  const windows: ParsedWindow[] = policies.map((p, i) => {
    const w = p.spec.schedule.windows?.[0];
    const startHour = parseInt(w?.start?.split(':')[0] || '0');
    const endHour = parseInt(w?.end?.split(':')[0] || '24');
    const days = w?.days ?? [0, 1, 2, 3, 4, 5, 6];
    return { name: p.metadata.name, color: POLICY_COLORS[i % POLICY_COLORS.length], days, startHour, endHour, policy: p };
  });

  const filteredWindows = selectedPolicy === 'all' ? windows : windows.filter(w => w.name === selectedPolicy);

  const getCellPolicies = (day: string, hour: number): ParsedWindow[] => {
    const dayNum = DAY_MAP[day];
    return filteredWindows.filter(w => {
      if (!w.days.includes(dayNum)) return false;
      return w.startHour <= w.endHour
        ? hour >= w.startHour && hour < w.endHour
        : hour >= w.startHour || hour < w.endHour;
    });
  };

  const openEdit = (policy: PolicyResponse) => {
    setEditPolicy(policy);
    onOpen();
  };

  const openCreate = () => {
    setEditPolicy(null);
    onOpen();
  };

  return (
    <VStack spacing={6} align="stretch">
      <HStack justify="space-between">
        <Box>
          <Heading size="lg">Schedule</Heading>
          <Text color="gray.500" fontSize="sm" mt={1}>Visual overview of power rules across the week</Text>
        </Box>
        <Select maxW="250px" value={selectedPolicy} onChange={e => setSelectedPolicy(e.target.value)} bg="white">
          <option value="all">All policies</option>
          {policies.map(p => <option key={p.metadata.name} value={p.metadata.name}>{p.metadata.name}</option>)}
        </Select>
      </HStack>

      {/* Legend */}
      <HStack spacing={4} flexWrap="wrap">
        {filteredWindows.map(w => (
          <HStack key={w.name} spacing={1}>
            <Box w={3} h={3} bg={`${w.color}.400`} borderRadius="sm" />
            <Text fontSize="xs" color="gray.600">{w.name} (ON)</Text>
          </HStack>
        ))}
        <HStack spacing={1}>
          <Box w={3} h={3} bg="gray.100" borderRadius="sm" border="1px solid" borderColor="gray.200" />
          <Text fontSize="xs" color="gray.500">OFF</Text>
        </HStack>
      </HStack>

      {/* Grid */}
      <Card shadow="sm" borderRadius="lg">
        <CardBody p={4} overflowX="auto">
          <Box minW="800px">
            <Flex>
              <Box w="50px" flexShrink={0} />
              {HOURS.map(h => (
                <Box key={h} flex={1} textAlign="center">
                  <Text fontSize="xs" color="gray.400">{h.toString().padStart(2, '0')}</Text>
                </Box>
              ))}
            </Flex>

            {DAYS.map(day => (
              <Flex key={day} align="center" h="32px" mt={1}>
                <Box w="50px" flexShrink={0}>
                  <Text fontSize="sm" fontWeight="medium" color="gray.600">{day}</Text>
                </Box>
                {HOURS.map(hour => {
                  const cellPolicies = getCellPolicies(day, hour);

                  if (cellPolicies.length === 0) {
                    return (
                      <ChakraTooltip key={hour} label={`${day} ${hour.toString().padStart(2, '0')}:00 — OFF (click to create)`} fontSize="xs" hasArrow>
                        <Box flex={1} h="26px" bg="gray.50" borderWidth="1px" borderColor="gray.100" borderRadius="2px" cursor="pointer" _hover={{ bg: 'gray.100' }} onClick={openCreate} />
                      </ChakraTooltip>
                    );
                  }

                  if (cellPolicies.length === 1) {
                    return (
                      <ChakraTooltip key={hour} label={`${day} ${hour.toString().padStart(2, '0')}:00 — ${cellPolicies[0].name} (ON)`} fontSize="xs" hasArrow>
                        <Box flex={1} h="26px" bg={`${cellPolicies[0].color}.100`} borderWidth="1px" borderColor={`${cellPolicies[0].color}.200`} borderRadius="2px" cursor="pointer" _hover={{ bg: `${cellPolicies[0].color}.200` }} onClick={() => openEdit(cellPolicies[0].policy)} />
                      </ChakraTooltip>
                    );
                  }

                  // Multiple policies overlap — split vertically, each clickable independently
                  return (
                    <Box key={hour} flex={1} h="26px" display="flex" flexDirection="column" borderWidth="1px" borderColor={`${cellPolicies[0].color}.200`} borderRadius="2px" overflow="hidden">
                      {cellPolicies.slice(0, 2).map((p, idx) => (
                        <ChakraTooltip key={idx} label={`${p.name} (ON) — click to edit`} fontSize="xs" hasArrow placement={idx === 0 ? 'top' : 'bottom'}>
                          <Box flex={1} bg={`${p.color}.200`} cursor="pointer" _hover={{ bg: `${p.color}.300` }} onClick={() => openEdit(p.policy)} />
                        </ChakraTooltip>
                      ))}
                    </Box>
                  );
                })}
              </Flex>
            ))}

            <Text fontSize="xs" color="gray.400" mt={2}>Click a cell to edit the policy or create a new one for that time slot.</Text>
          </Box>
        </CardBody>
      </Card>

      <CreateRuleDrawer isOpen={isOpen} onClose={() => { onClose(); setEditPolicy(null); }} editPolicy={editPolicy} />
    </VStack>
  );
}
