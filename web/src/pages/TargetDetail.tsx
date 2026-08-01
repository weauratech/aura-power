import { Heading, Card, CardBody, SimpleGrid, Text, Badge, Divider, Spinner, Alert, AlertIcon, List, ListItem, VStack, HStack, Box, Code, Flex } from '@chakra-ui/react';
import { useParams } from 'react-router-dom';
import { useExplainTarget, useAuditEvents } from '../hooks/useApi';
import { StatusBadge } from '../components/StatusBadge';

interface ExplainResponse {
  ref: { namespace: string; name: string; kind: string };
  effectiveState: string;
  observedState: { powerState: string; replicas: number };
  winningRule?: { kind: string; name: string; priority: number; description?: string };
  suppressedRules?: Array<{ kind: string; name: string; priority: number }>;
  blocked: boolean;
  blockReasons?: Array<{ type: string; message: string; waivable: boolean }>;
  snapshot?: { available: boolean; replicaCount?: number; resources?: { cpuMillicores: number; memoryMiB: number } };
  ownership?: Array<{ type: string; optedIn: boolean }>;
  savings?: { cpuHoursSaved: number; estimatedCost: number };
  lastTransition?: string;
}

export function TargetDetail() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const { data, isLoading, error } = useExplainTarget(namespace ?? '', name ?? '');
  const { data: auditData } = useAuditEvents(namespace, name);

  if (isLoading) return <Flex justify="center" align="center" py={20}><Spinner size="xl" color="blue.500" /><Text ml={3} color="gray.500">Loading target details...</Text></Flex>;
  if (error) return <Alert status="error" borderRadius="lg"><AlertIcon />Failed to load target details</Alert>;
  if (!data) return <Alert status="warning" borderRadius="lg"><AlertIcon />Target not found</Alert>;

  const target = data as ExplainResponse;
  const recentEvents = (auditData?.events ?? []).slice(0, 5);

  return (
    <VStack spacing={6} align="stretch">
      {/* Hero section */}
      <Card shadow="sm" borderRadius="lg" bg="white">
        <CardBody>
          <HStack justify="space-between" align="start">
            <Box>
              <Heading size="lg" color="gray.800">{name}</Heading>
              <HStack spacing={3} mt={2}>
                <Text color="gray.500" fontSize="sm">{namespace}</Text>
                <Badge variant="subtle" colorScheme="gray" fontSize="xs">{target.ref?.kind}</Badge>
              </HStack>
            </Box>
            <Box>
              <StatusBadge state={target.effectiveState ?? 'unknown'} />
            </Box>
          </HStack>
        </CardBody>
      </Card>

      {/* Blocked alert */}
      {target.blocked && (
        <Alert status="error" borderRadius="lg" data-testid="target-blocked-alert">
          <AlertIcon />
          <Box>
            <Text fontWeight="bold" color="gray.700">This workload is blocked</Text>
            <Text fontSize="sm" color="gray.600">Power management cannot act on this workload due to guardrail violations.</Text>
          </Box>
        </Alert>
      )}

      {/* Managed by */}
      {target.winningRule && !target.blocked && (
        <Alert status="info" borderRadius="lg" variant="left-accent" data-testid="target-managed-by">
          <AlertIcon />
          <Box>
            <Text fontWeight="medium" color="gray.700">Managed by: {target.winningRule.name}</Text>
            {target.winningRule.description && <Text fontSize="sm" color="gray.600">{target.winningRule.description}</Text>}
            <Text fontSize="xs" color="gray.500">Priority: {target.winningRule.priority} | Kind: {target.winningRule.kind}</Text>
          </Box>
        </Alert>
      )}

      <SimpleGrid columns={{ base: 1, md: 2 }} spacing={6}>
        {/* State Card */}
        <Card shadow="sm" borderRadius="lg">
          <CardBody>
            <Heading size="sm" mb={4} color="gray.700">State</Heading>
            <SimpleGrid columns={2} spacing={3}>
              <Text fontWeight="semibold" fontSize="sm" color="gray.600">Effective:</Text>
              <StatusBadge state={target.effectiveState ?? 'unknown'} />
              <Text fontWeight="semibold" fontSize="sm" color="gray.600">Observed:</Text>
              <StatusBadge state={target.observedState?.powerState ?? 'unknown'} />
              <Text fontWeight="semibold" fontSize="sm" color="gray.600">Blocked:</Text>
              <Text fontSize="sm" color={target.blocked ? 'red.500' : 'gray.700'}>{target.blocked ? 'Yes' : 'No'}</Text>
            </SimpleGrid>
          </CardBody>
        </Card>

        {/* Block Reasons */}
        {target.blockReasons && target.blockReasons.length > 0 && (
          <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="red.400">
            <CardBody>
              <Heading size="sm" mb={4} color="red.600">Block Reasons</Heading>
              <List spacing={3}>
                {target.blockReasons.map((b, i) => (
                  <ListItem key={i}>
                    <HStack align="start">
                      <Badge variant="subtle" colorScheme={b.waivable ? 'yellow' : 'red'} mt={0.5}>
                        {b.type}
                      </Badge>
                      <VStack align="start" spacing={0}>
                        <Text fontSize="sm" color="gray.700">{b.message}</Text>
                        {b.waivable && (
                          <Text fontSize="xs" color="orange.600">
                            Waivable — add <Code fontSize="xs">aura.sh/power-eligible=true</Code> to unblock
                          </Text>
                        )}
                      </VStack>
                    </HStack>
                  </ListItem>
                ))}
              </List>
            </CardBody>
          </Card>
        )}

        {/* Snapshot */}
        {target.snapshot && target.snapshot.available && (
          <Card shadow="sm" borderRadius="lg">
            <CardBody>
              <Heading size="sm" mb={4} color="gray.700">Snapshot</Heading>
              <VStack align="stretch" spacing={2}>
                {target.snapshot.replicaCount != null && (
                  <HStack>
                    <Text fontSize="sm" fontWeight="semibold" color="gray.600">Replica Count:</Text>
                    {target.snapshot.replicaCount === 0 ? (
                      <Badge variant="subtle" colorScheme="orange">0 (powered off)</Badge>
                    ) : (
                      <Text fontSize="sm" color="gray.700">{target.snapshot.replicaCount}</Text>
                    )}
                  </HStack>
                )}
                {target.snapshot.resources && (
                  <>
                    {target.snapshot.resources.cpuMillicores > 0 && (
                      <HStack>
                        <Text fontSize="sm" fontWeight="semibold" color="gray.600">CPU:</Text>
                        <Text fontSize="sm" color="gray.700">{target.snapshot.resources.cpuMillicores}m</Text>
                      </HStack>
                    )}
                    {target.snapshot.resources.memoryMiB > 0 && (
                      <HStack>
                        <Text fontSize="sm" fontWeight="semibold" color="gray.600">Memory:</Text>
                        <Text fontSize="sm" color="gray.700">{target.snapshot.resources.memoryMiB}Mi</Text>
                      </HStack>
                    )}
                  </>
                )}
              </VStack>
            </CardBody>
          </Card>
        )}

        {/* Savings */}
        {target.savings && (
          <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="green.400">
            <CardBody>
              <Heading size="sm" mb={4} color="gray.700">Savings</Heading>
              <VStack align="stretch" spacing={2}>
                <HStack>
                  <Text fontSize="sm" fontWeight="semibold" color="gray.600">CPU Hours:</Text>
                  <Text fontSize="sm" color="gray.700">{target.savings.cpuHoursSaved.toFixed(1)}</Text>
                </HStack>
                <HStack>
                  <Text fontSize="sm" fontWeight="semibold" color="gray.600">Est. Cost:</Text>
                  <Text fontSize="sm" fontWeight="bold" color="green.600">${target.savings.estimatedCost.toFixed(2)}</Text>
                </HStack>
              </VStack>
            </CardBody>
          </Card>
        )}

        {/* Ownership */}
        {target.ownership && target.ownership.length > 0 && (
          <Card shadow="sm" borderRadius="lg">
            <CardBody>
              <Heading size="sm" mb={4} color="gray.700">Ownership</Heading>
              <List spacing={2}>
                {target.ownership.map((o, i) => (
                  <ListItem key={i}>
                    <HStack>
                      <Text fontSize="sm" color="gray.700">{o.type}</Text>
                      <Badge variant="subtle" colorScheme={o.optedIn ? 'green' : 'gray'} fontSize="xs">
                        {o.optedIn ? 'opted in' : 'not opted in'}
                      </Badge>
                    </HStack>
                  </ListItem>
                ))}
              </List>
            </CardBody>
          </Card>
        )}
      </SimpleGrid>

      {/* Recent Events */}
      {recentEvents.length > 0 && (
        <>
          <Divider />
          <Card shadow="sm" borderRadius="lg" data-testid="target-recent-events">
            <CardBody>
              <Heading size="sm" mb={4} color="gray.700">Recent Events</Heading>
              <VStack align="stretch" spacing={3}>
                {recentEvents.map((evt, i) => (
                  <Box key={i} p={3} bg="gray.50" borderRadius="md" borderLeftWidth="3px" borderLeftColor={evt.spec.result === 'success' ? 'green.400' : 'red.400'}>
                    <HStack justify="space-between" mb={1}>
                      <HStack spacing={2}>
                        <Badge variant="subtle" colorScheme={evt.spec.result === 'success' ? 'green' : 'red'} fontSize="xs">
                          {evt.spec.action}
                        </Badge>
                        {evt.spec.ruleName && <Text fontSize="xs" color="gray.500">rule: {evt.spec.ruleName}</Text>}
                      </HStack>
                      <Text fontSize="xs" color="gray.400">
                        {new Date(evt.spec.timestamp).toLocaleString()}
                      </Text>
                    </HStack>
                    <Text fontSize="sm" color="gray.700">{evt.spec.reason}</Text>
                    <Text fontSize="xs" color="gray.500">Actor: {evt.spec.actor} | Result: {evt.spec.result}</Text>
                  </Box>
                ))}
              </VStack>
            </CardBody>
          </Card>
        </>
      )}

      {target.lastTransition && (
        <>
          <Divider />
          <Text fontSize="sm" color="gray.500">
            Last transition: {new Date(target.lastTransition).toLocaleString()}
          </Text>
        </>
      )}
    </VStack>
  );
}
