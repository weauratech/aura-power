import {
  Box, Heading, Table, Thead, Tbody, Tr, Th, Td, Select, HStack, Input, Spinner,
  Alert, AlertIcon, Button, VStack, Text, Badge, Flex, useDisclosure, SimpleGrid,
  Card, CardBody, Stat, StatLabel, StatNumber
} from '@chakra-ui/react';
import { useState, useMemo } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useTargets, useNamespaces } from '../hooks/useApi';
import { CreateRuleDrawer } from '../components/CreateRuleDrawer';

type SortField = 'namespace' | 'name' | 'kind' | 'state' | 'rule';
type SortDir = 'asc' | 'desc';

const SYSTEM_NS = ['kube-system', 'kube-public', 'kube-node-lease'];

function getEffectiveState(t: any): string {
  if (t.status?.blocked) return 'blocked';
  if (t.status?.divergent) return 'divergent';
  if (t.status?.desiredState === 'off') return 'off';
  if (t.status?.desiredState === 'on') return 'on';
  return 'unmanaged';
}

function stateOrder(state: string): number {
  switch (state) {
    case 'blocked': return 0;
    case 'divergent': return 1;
    case 'off': return 2;
    case 'on': return 3;
    case 'unmanaged': return 4;
    default: return 5;
  }
}

function StateDot({ state }: { state: string }) {
  const color = {
    on: 'green.400',
    off: 'red.400',
    blocked: 'red.600',
    divergent: 'orange.400',
    unmanaged: 'gray.300',
  }[state] || 'gray.300';

  const label = {
    on: 'On',
    off: 'Off',
    blocked: 'Blocked',
    divergent: 'Divergent',
    unmanaged: 'Unmanaged',
  }[state] || state;

  return (
    <HStack spacing={2}>
      <Box w="8px" h="8px" borderRadius="full" bg={color} flexShrink={0} />
      <Text fontSize="sm" color="gray.700">{label}</Text>
    </HStack>
  );
}

function SortIcon({ field, sortField, sortDir }: { field: SortField; sortField: SortField; sortDir: SortDir }) {
  if (field !== sortField) return <Text as="span" color="gray.300" ml={1}>↕</Text>;
  return <Text as="span" color="blue.500" ml={1}>{sortDir === 'asc' ? '↑' : '↓'}</Text>;
}

export function Targets() {
  const [namespace, setNamespace] = useState('');
  const [stateFilter, setStateFilter] = useState('');
  const [search, setSearch] = useState('');
  const [sortField, setSortField] = useState<SortField>('state');
  const [sortDir, setSortDir] = useState<SortDir>('asc');
  const { data, isLoading, error } = useTargets(namespace || undefined, undefined);
  const { data: nsData } = useNamespaces();
  const { isOpen, onOpen, onClose } = useDisclosure();
  const navigate = useNavigate();

  if (error) return <Alert status="error" borderRadius="lg"><AlertIcon />Failed to load targets</Alert>;

  const namespaces = (nsData?.namespaces ?? []).filter(ns => !SYSTEM_NS.includes(ns)).sort();
  const allTargets = data?.targets ?? [];

  // Compute stats
  const stats = useMemo(() => {
    const s = { total: 0, on: 0, off: 0, blocked: 0, divergent: 0, unmanaged: 0 };
    for (const t of allTargets) {
      s.total++;
      const state = getEffectiveState(t);
      if (state in s) (s as any)[state]++;
    }
    return s;
  }, [allTargets]);

  // Filter
  const filtered = useMemo(() => {
    return allTargets.filter(t => {
      if (stateFilter && getEffectiveState(t) !== stateFilter) return false;
      if (search) {
        const s = search.toLowerCase();
        if (!t.spec.targetRef.name.toLowerCase().includes(s) &&
          !t.spec.targetRef.namespace.toLowerCase().includes(s)) return false;
      }
      return true;
    });
  }, [allTargets, stateFilter, search]);

  // Sort
  const sorted = useMemo(() => {
    return [...filtered].sort((a, b) => {
      let cmp = 0;
      switch (sortField) {
        case 'namespace':
          cmp = a.spec.targetRef.namespace.localeCompare(b.spec.targetRef.namespace);
          break;
        case 'name':
          cmp = a.spec.targetRef.name.localeCompare(b.spec.targetRef.name);
          break;
        case 'kind':
          cmp = a.spec.targetRef.kind.localeCompare(b.spec.targetRef.kind);
          break;
        case 'state':
          cmp = stateOrder(getEffectiveState(a)) - stateOrder(getEffectiveState(b));
          break;
        case 'rule':
          const ra = a.status?.winningRule?.name || 'zzz';
          const rb = b.status?.winningRule?.name || 'zzz';
          cmp = ra.localeCompare(rb);
          break;
      }
      return sortDir === 'asc' ? cmp : -cmp;
    });
  }, [filtered, sortField, sortDir]);

  const toggleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc');
    } else {
      setSortField(field);
      setSortDir('asc');
    }
  };

  const uniqueNamespaces = useMemo(() => {
    const ns = new Set(allTargets.map(t => t.spec.targetRef.namespace));
    return Array.from(ns).sort();
  }, [allTargets]);

  return (
    <VStack spacing={5} align="stretch">
      {/* Header */}
      <HStack justify="space-between">
        <Box>
          <Heading size="lg">Targets</Heading>
          <Text color="gray.500" fontSize="sm" mt={1}>
            {stats.total} workloads across {uniqueNamespaces.length} namespaces
          </Text>
        </Box>
        <Button colorScheme="blue" size="sm" onClick={onOpen}>+ New Rule</Button>
      </HStack>

      {/* Stat Cards */}
      <SimpleGrid columns={{ base: 3, md: 5 }} spacing={3}>
        <Card shadow="sm" borderRadius="lg" cursor="pointer" onClick={() => setStateFilter('')}
          borderWidth={!stateFilter ? 2 : 0} borderColor="blue.400"
          transition="all 0.15s" _hover={{ shadow: 'md' }}>
          <CardBody py={3} px={4}>
            <Stat size="sm">
              <StatNumber fontSize="xl" color="gray.800">{stats.total}</StatNumber>
              <StatLabel fontSize="xs" color="gray.500">Total</StatLabel>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" cursor="pointer" onClick={() => setStateFilter('on')}
          borderWidth={stateFilter === 'on' ? 2 : 0} borderColor="green.400"
          transition="all 0.15s" _hover={{ shadow: 'md' }}>
          <CardBody py={3} px={4}>
            <Stat size="sm">
              <StatNumber fontSize="xl" color="green.600">{stats.on}</StatNumber>
              <StatLabel fontSize="xs" color="gray.500">On</StatLabel>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" cursor="pointer" onClick={() => setStateFilter('off')}
          borderWidth={stateFilter === 'off' ? 2 : 0} borderColor="red.400"
          transition="all 0.15s" _hover={{ shadow: 'md' }}>
          <CardBody py={3} px={4}>
            <Stat size="sm">
              <StatNumber fontSize="xl" color="red.500">{stats.off}</StatNumber>
              <StatLabel fontSize="xs" color="gray.500">Off</StatLabel>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" cursor="pointer" onClick={() => setStateFilter('blocked')}
          borderWidth={stateFilter === 'blocked' ? 2 : 0} borderColor="red.600"
          transition="all 0.15s" _hover={{ shadow: 'md' }}>
          <CardBody py={3} px={4}>
            <Stat size="sm">
              <StatNumber fontSize="xl" color="red.700">{stats.blocked}</StatNumber>
              <StatLabel fontSize="xs" color="gray.500">Blocked</StatLabel>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" cursor="pointer" onClick={() => setStateFilter('divergent')}
          borderWidth={stateFilter === 'divergent' ? 2 : 0} borderColor="orange.400"
          transition="all 0.15s" _hover={{ shadow: 'md' }}>
          <CardBody py={3} px={4}>
            <Stat size="sm">
              <StatNumber fontSize="xl" color="orange.500">{stats.divergent}</StatNumber>
              <StatLabel fontSize="xs" color="gray.500">Divergent</StatLabel>
            </Stat>
          </CardBody>
        </Card>
      </SimpleGrid>

      {/* Filters */}
      <HStack spacing={3}>
        <Input
          placeholder="Search workload or namespace..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          bg="white"
          size="sm"
          maxW="280px"
        />
        <Select placeholder="All namespaces" value={namespace} onChange={e => setNamespace(e.target.value)} size="sm" maxW="200px" bg="white">
          {namespaces.map(ns => <option key={ns} value={ns}>{ns}</option>)}
        </Select>
        {(namespace || stateFilter || search) && (
          <Button size="xs" variant="ghost" colorScheme="blue" onClick={() => { setNamespace(''); setStateFilter(''); setSearch(''); }}>
            Clear filters
          </Button>
        )}
      </HStack>

      {/* Table */}
      {isLoading ? (
        <Flex justify="center" align="center" py={12}>
          <Spinner size="lg" color="blue.500" />
          <Text ml={3} color="gray.500">Loading targets...</Text>
        </Flex>
      ) : sorted.length === 0 ? (
        <Card shadow="sm" borderRadius="lg">
          <CardBody textAlign="center" py={10}>
            <Text color="gray.500" mb={2}>No targets match your filters</Text>
            <Button size="sm" variant="ghost" colorScheme="blue" onClick={() => { setNamespace(''); setStateFilter(''); setSearch(''); }}>
              Clear filters
            </Button>
          </CardBody>
        </Card>
      ) : (
        <>
          <Box overflowX="auto" bg="white" borderRadius="lg" shadow="sm">
            <Table size="sm">
              <Thead>
                <Tr>
                  <Th fontSize="xs" cursor="pointer" onClick={() => toggleSort('namespace')} userSelect="none" _hover={{ color: 'blue.600' }}>
                    Namespace <SortIcon field="namespace" sortField={sortField} sortDir={sortDir} />
                  </Th>
                  <Th fontSize="xs" cursor="pointer" onClick={() => toggleSort('name')} userSelect="none" _hover={{ color: 'blue.600' }}>
                    Workload <SortIcon field="name" sortField={sortField} sortDir={sortDir} />
                  </Th>
                  <Th fontSize="xs" cursor="pointer" onClick={() => toggleSort('kind')} userSelect="none" _hover={{ color: 'blue.600' }}>
                    Kind <SortIcon field="kind" sortField={sortField} sortDir={sortDir} />
                  </Th>
                  <Th fontSize="xs" cursor="pointer" onClick={() => toggleSort('state')} userSelect="none" _hover={{ color: 'blue.600' }}>
                    State <SortIcon field="state" sortField={sortField} sortDir={sortDir} />
                  </Th>
                  <Th fontSize="xs" cursor="pointer" onClick={() => toggleSort('rule')} userSelect="none" _hover={{ color: 'blue.600' }}>
                    Rule <SortIcon field="rule" sortField={sortField} sortDir={sortDir} />
                  </Th>
                </Tr>
              </Thead>
              <Tbody>
                {sorted.map(t => {
                  const state = getEffectiveState(t);
                  const ref = t.spec.targetRef;
                  return (
                    <Tr
                      key={`${ref.namespace}/${ref.name}`}
                      _hover={{ bg: 'blue.50' }}
                      transition="all 0.1s"
                      cursor="pointer"
                      onClick={() => navigate(`/targets/${ref.namespace}`)}
                    >
                      <Td fontSize="sm">
                        <Text color="blue.600" fontWeight="medium">{ref.namespace}</Text>
                      </Td>
                      <Td fontSize="sm">
                        <Text color="gray.800" fontWeight="medium">{ref.name}</Text>
                      </Td>
                      <Td>
                        <Badge variant="subtle" colorScheme="gray" fontSize="xs" textTransform="capitalize">{ref.kind}</Badge>
                      </Td>
                      <Td>
                        <StateDot state={state} />
                      </Td>
                      <Td>
                        {t.status?.winningRule ? (
                          <Link to={`/rules/${t.status.winningRule.name}`} onClick={e => e.stopPropagation()}>
                            <Badge variant="subtle" colorScheme="blue" fontSize="xs" cursor="pointer" _hover={{ bg: 'blue.100' }}>
                              {t.status.winningRule.name}
                            </Badge>
                          </Link>
                        ) : (
                          <Text fontSize="xs" color="gray.300">{'\u2014'}</Text>
                        )}
                      </Td>
                    </Tr>
                  );
                })}
              </Tbody>
            </Table>
          </Box>

          {/* Footer */}
          <Text fontSize="xs" color="gray.400" textAlign="right">
            Showing {sorted.length} of {stats.total} targets
          </Text>
        </>
      )}

      <CreateRuleDrawer isOpen={isOpen} onClose={onClose} />
    </VStack>
  );
}
