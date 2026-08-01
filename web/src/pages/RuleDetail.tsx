import { Box, Card, CardBody, Heading, SimpleGrid, Spinner, Alert, AlertIcon, Text, Badge, VStack, HStack, Stat, StatLabel, StatNumber, Wrap, WrapItem, Divider, Flex } from '@chakra-ui/react';
import { useParams, Link } from 'react-router-dom';
import { usePolicies, PolicyResponse } from '../hooks/useApi';

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export function RuleDetail() {
  const { name } = useParams<{ name: string }>();
  const { data, isLoading, error } = usePolicies();

  if (isLoading) return <Flex justify="center" align="center" py={20}><Spinner size="xl" color="blue.500" /><Text ml={3} color="gray.500">Loading rule details...</Text></Flex>;
  if (error) return <Alert status="error" borderRadius="lg"><AlertIcon />Failed to load policies</Alert>;

  const policy = data?.items?.find((p: PolicyResponse) => p.metadata.name === name);

  if (!policy) {
    return (
      <Alert status="warning" borderRadius="lg" data-testid="rule-not-found">
        <AlertIcon />
        Policy &ldquo;{name}&rdquo; not found. <Link to="/rules"><Text as="span" color="blue.500" ml={2} textDecoration="underline">Back to Rules</Text></Link>
      </Alert>
    );
  }

  const window = policy.spec.schedule.windows?.[0];
  const createdDate = new Date(policy.metadata.creationTimestamp).toLocaleDateString('en-US', {
    year: 'numeric', month: 'long', day: 'numeric',
  });

  return (
    <VStack spacing={6} align="stretch">
      <Box>
        <HStack mb={1}>
          <Link to="/rules"><Text fontSize="sm" color="blue.500" _hover={{ textDecoration: 'underline' }}>Rules</Text></Link>
          <Text fontSize="sm" color="gray.400">/</Text>
          <Text fontSize="sm" color="gray.600">{policy.metadata.name}</Text>
        </HStack>
        <Heading size="lg" data-testid="rule-detail-name">{policy.metadata.name}</Heading>
        {policy.spec.description && (
          <Text color="gray.500" mt={1}>{policy.spec.description}</Text>
        )}
      </Box>

      {/* Stats Row */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="purple.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">Priority</StatLabel>
              <StatNumber fontSize="2xl" color="purple.500">{policy.spec.priority}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg" borderLeftWidth="3px" borderLeftColor="blue.400">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">Affected Targets</StatLabel>
              <StatNumber fontSize="2xl" color="blue.500" data-testid="rule-affected-targets">
                {policy.status?.affectedTargets ?? '\u2014'}
              </StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">Created</StatLabel>
              <StatNumber fontSize="md" color="gray.700">{createdDate}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
        <Card shadow="sm" borderRadius="lg">
          <CardBody py={3} px={4}>
            <Stat>
              <StatLabel fontSize="xs" color="gray.500" textTransform="uppercase" letterSpacing="wide">Namespace</StatLabel>
              <StatNumber fontSize="md" color="gray.700">{policy.metadata.namespace}</StatNumber>
            </Stat>
          </CardBody>
        </Card>
      </SimpleGrid>

      <SimpleGrid columns={{ base: 1, md: 2 }} spacing={6}>
        {/* Scope Card */}
        <Card shadow="sm" borderRadius="lg">
          <CardBody>
            <Heading size="sm" mb={3} color="gray.700">Scope</Heading>
            <Text fontSize="sm" color="gray.500" mb={2}>Governed namespaces:</Text>
            <Wrap spacing={2}>
              {(policy.spec.scope.namespaces && policy.spec.scope.namespaces.length > 0)
                ? policy.spec.scope.namespaces.map(ns => (
                  <WrapItem key={ns}>
                    <Badge variant="subtle" colorScheme="blue" fontSize="sm" px={2} py={1} borderRadius="md">{ns}</Badge>
                  </WrapItem>
                ))
                : <Text fontSize="sm" color="gray.400">All namespaces</Text>
              }
            </Wrap>
          </CardBody>
        </Card>

        {/* Schedule Visualization Card */}
        <Card shadow="sm" borderRadius="lg">
          <CardBody>
            <Heading size="sm" mb={3} color="gray.700">Schedule</Heading>
            {window ? (
              <VStack align="stretch" spacing={3}>
                <HStack>
                  <Badge variant="subtle" colorScheme="green" fontSize="xs">ON</Badge>
                  <Text fontSize="sm" color="gray.700">{window.start} &ndash; {window.end}</Text>
                  <Text fontSize="xs" color="gray.500">({window.timezone})</Text>
                </HStack>
                <HStack>
                  <Badge variant="subtle" colorScheme="gray" fontSize="xs">OFF</Badge>
                  <Text fontSize="sm" color="gray.700">Outside window hours</Text>
                </HStack>
                <Divider />
                <Text fontSize="xs" color="gray.500" fontWeight="medium" textTransform="uppercase" letterSpacing="wide">Active days:</Text>
                <HStack spacing={1}>
                  {WEEKDAYS.map((day, i) => {
                    const isActive = (window.days ?? [0, 1, 2, 3, 4, 5, 6]).includes(i);
                    return (
                      <Badge
                        key={i}
                        colorScheme={isActive ? 'blue' : 'gray'}
                        variant={isActive ? 'solid' : 'outline'}
                        fontSize="xs"
                        px={2}
                        py={1}
                      >
                        {day}
                      </Badge>
                    );
                  })}
                </HStack>
              </VStack>
            ) : (
              <VStack align="start" spacing={2}>
                <HStack>
                  <Badge variant="subtle" colorScheme="green">Always ON</Badge>
                </HStack>
                <Text fontSize="sm" color="gray.500">
                  This policy keeps workloads powered on at all times (desired state: {policy.spec.schedule.desiredState})
                </Text>
              </VStack>
            )}
          </CardBody>
        </Card>
      </SimpleGrid>
    </VStack>
  );
}
