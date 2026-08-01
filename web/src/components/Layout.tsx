import { Box, Flex, Text, VStack, Divider, Button } from '@chakra-ui/react';
import { Link, Outlet, useLocation } from 'react-router-dom';

interface NavItem {
  path: string;
  label: string;
  testId: string;
}

interface LayoutProps {
  user?: { username: string; role: string } | null;
  onLogout?: () => void;
}

const NAV_ITEMS: NavItem[] = [
  { path: '/', label: 'Dashboard', testId: 'nav-dashboard' },
  { path: '/targets', label: 'Targets', testId: 'nav-targets' },
  { path: '/rules', label: 'Rules', testId: 'nav-rules' },
  { path: '/schedule', label: 'Schedule', testId: 'nav-schedule' },
  { path: '/metrics', label: 'Metrics', testId: 'nav-metrics' },
  { path: '/blocked', label: 'Blocked', testId: 'nav-blocked' },
  { path: '/savings', label: 'Savings', testId: 'nav-savings' },
];

function isActive(currentPath: string, itemPath: string): boolean {
  if (itemPath === '/') return currentPath === '/';
  return currentPath.startsWith(itemPath);
}

export function Layout({ user, onLogout }: LayoutProps) {
  const location = useLocation();
  const showPending = user && (user.role === 'approver' || user.role === 'admin');
  const showUsers = user && user.role === 'admin';

  return (
    <Flex minH="100vh">
      <Box
        as="nav"
        w="240px"
        minH="100vh"
        bg="gray.900"
        color="white"
        position="fixed"
        top={0}
        left={0}
        bottom={0}
        display="flex"
        flexDirection="column"
        aria-label="Main navigation"
      >
        <Flex align="center" px={5} py={5} mb={2}>
          <Text fontSize="lg" fontWeight="bold" letterSpacing="tight">Aura Power</Text>
        </Flex>

        <VStack spacing={1} align="stretch" flex={1} px={3}>
          {NAV_ITEMS.map((item) => {
            const active = isActive(location.pathname, item.path);
            return (
              <Link key={item.path} to={item.path} data-testid={item.testId}>
                <Flex
                  align="center"
                  px={3}
                  py={2.5}
                  borderRadius="md"
                  borderLeftWidth="3px"
                  borderLeftColor={active ? 'blue.400' : 'transparent'}
                  bg={active ? 'whiteAlpha.100' : 'transparent'}
                  color={active ? 'white' : 'whiteAlpha.700'}
                  fontWeight={active ? 'semibold' : 'normal'}
                  cursor="pointer"
                  transition="all 0.2s"
                  _hover={{ bg: 'whiteAlpha.50', color: 'white' }}
                  _focusVisible={{ outline: '2px solid', outlineColor: 'blue.400', outlineOffset: '2px' }}
                  aria-current={active ? 'page' : undefined}
                >
                  <Text fontSize="sm">{item.label}</Text>
                </Flex>
              </Link>
            );
          })}

          {showPending && (
            <>
              <Divider borderColor="whiteAlpha.200" my={2} />
              <Link to="/pending" data-testid="nav-pending">
                <Flex
                  align="center"
                  px={3}
                  py={2.5}
                  borderRadius="md"
                  borderLeftWidth="3px"
                  borderLeftColor={isActive(location.pathname, '/pending') ? 'orange.400' : 'transparent'}
                  bg={isActive(location.pathname, '/pending') ? 'whiteAlpha.100' : 'transparent'}
                  color={isActive(location.pathname, '/pending') ? 'white' : 'whiteAlpha.700'}
                  fontWeight={isActive(location.pathname, '/pending') ? 'semibold' : 'normal'}
                  cursor="pointer"
                  transition="all 0.2s"
                  _hover={{ bg: 'whiteAlpha.50', color: 'white' }}
                >
                  <Text fontSize="sm">Pending</Text>
                </Flex>
              </Link>
            </>
          )}

          {showUsers && (
            <Link to="/users" data-testid="nav-users">
              <Flex
                align="center"
                px={3}
                py={2.5}
                borderRadius="md"
                borderLeftWidth="3px"
                borderLeftColor={isActive(location.pathname, '/users') ? 'purple.400' : 'transparent'}
                bg={isActive(location.pathname, '/users') ? 'whiteAlpha.100' : 'transparent'}
                color={isActive(location.pathname, '/users') ? 'white' : 'whiteAlpha.700'}
                fontWeight={isActive(location.pathname, '/users') ? 'semibold' : 'normal'}
                cursor="pointer"
                transition="all 0.2s"
                _hover={{ bg: 'whiteAlpha.50', color: 'white' }}
              >
                <Text fontSize="sm">Users</Text>
              </Flex>
            </Link>
          )}
        </VStack>

        <Box px={4} py={4} borderTopWidth="1px" borderTopColor="whiteAlpha.100">
          {user && (
            <Flex align="center" justify="space-between" mb={2}>
              <Box>
                <Text fontSize="xs" fontWeight="medium" color="white">{user.username}</Text>
                <Text fontSize="xs" color="gray.400">{user.role}</Text>
              </Box>
              <Button
                size="xs"
                variant="ghost"
                color="whiteAlpha.600"
                _hover={{ color: 'white', bg: 'whiteAlpha.100' }}
                onClick={onLogout}
              >
                Sign out
              </Button>
            </Flex>
          )}
          <Text fontSize="xs" color="gray.600">Aura Power v2.0</Text>
        </Box>
      </Box>

      <Box as="main" flex={1} ml="240px" bg="gray.50" minH="100vh" p={6}>
        <Outlet />
      </Box>
    </Flex>
  );
}
