import { Box, Button, Card, CardBody, FormControl, FormLabel, Heading, Input, Text, VStack, Alert, AlertIcon, Flex } from '@chakra-ui/react';
import { useState } from 'react';
import { friendlyError } from '../utils/errors';

interface LoginProps {
  onLogin: () => void;
}

export function Login({ onLogin }: LoginProps) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      const res = await fetch('/api/v1/auth/login', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });

      if (!res.ok) {
        const data = await res.json();
        setError(friendlyError(data.error || 'Login failed'));
        return;
      }

      // Cookie is set automatically by the server (HttpOnly)
      // No need to store token in localStorage
      onLogin();
    } catch {
      setError(friendlyError('Connection failed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Flex minH="100vh" align="center" justify="center" bg="gray.50">
      <Card shadow="lg" borderRadius="lg" w="100%" maxW="400px" mx={4}>
        <CardBody p={8}>
          <VStack spacing={6} align="stretch">
            <Box textAlign="center">
              <Heading size="lg" color="gray.800">Aura Power</Heading>
              <Text color="gray.500" fontSize="sm" mt={1}>Sign in to continue</Text>
            </Box>

            {error && (
              <Alert status="error" borderRadius="md" fontSize="sm">
                <AlertIcon />{error}
              </Alert>
            )}

            <form onSubmit={handleSubmit}>
              <VStack spacing={4}>
                <FormControl isRequired>
                  <FormLabel fontSize="sm">Username</FormLabel>
                  <Input value={username} onChange={e => setUsername(e.target.value)} autoFocus data-testid="login-username" />
                </FormControl>
                <FormControl isRequired>
                  <FormLabel fontSize="sm">Password</FormLabel>
                  <Input type="password" value={password} onChange={e => setPassword(e.target.value)} data-testid="login-password" />
                </FormControl>
                <Button type="submit" colorScheme="blue" w="100%" isLoading={loading} data-testid="login-submit">Sign in</Button>
              </VStack>
            </form>
          </VStack>
        </CardBody>
      </Card>
    </Flex>
  );
}
