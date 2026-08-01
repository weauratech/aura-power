import {
  Box, Heading, Table, Thead, Tbody, Tr, Th, Td, Button, VStack, Text,
  Flex, Card, CardBody, useToast,
  Modal, ModalOverlay, ModalContent, ModalHeader, ModalBody, ModalFooter, ModalCloseButton,
  FormControl, FormLabel, Input, Select, useDisclosure, AlertDialog, AlertDialogOverlay,
  AlertDialogContent, AlertDialogHeader, AlertDialogBody, AlertDialogFooter
} from '@chakra-ui/react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useState, useRef } from 'react';
import { friendlyError } from '../utils/errors';

interface User {
  id: string;
  username: string;
  role: 'member' | 'approver' | 'admin';
}

export function Users() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { isOpen: isCreateOpen, onOpen: onCreateOpen, onClose: onCreateClose } = useDisclosure();
  const { isOpen: isDeleteOpen, onOpen: onDeleteOpen, onClose: onDeleteClose } = useDisclosure();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);
  const [newUser, setNewUser] = useState({ username: '', password: '', role: 'member' });

  const { data, isLoading, error } = useQuery<{ users: User[]; count: number }>({
    queryKey: ['users'],
    queryFn: async () => {
      const res = await fetch('/api/v1/users', { credentials: 'same-origin' });
      if (!res.ok) throw new Error('Failed to load users');
      return res.json();
    },
  });

  const createMutation = useMutation({
    mutationFn: async (userData: { username: string; password: string; role: string }) => {
      const res = await fetch('/api/v1/users', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(userData),
      });
      if (!res.ok) {
        const err = await res.json();
        throw new Error(friendlyError(err.error || 'Failed to create user'));
      }
      return res.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      toast({ title: 'User created', status: 'success', duration: 3000 });
      onCreateClose();
      setNewUser({ username: '', password: '', role: 'member' });
    },
    onError: (err: Error) => {
      toast({ title: 'Error', description: err.message, status: 'error', duration: 5000 });
    },
  });

  const updateRoleMutation = useMutation({
    mutationFn: async ({ id, role }: { id: string; role: string }) => {
      const res = await fetch(`/api/v1/users/${id}`, {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role }),
      });
      if (!res.ok) throw new Error('Failed to update');
      return res.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      toast({ title: 'Role updated', status: 'success', duration: 3000 });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async (id: string) => {
      const res = await fetch(`/api/v1/users/${id}`, {
        method: 'DELETE',
        credentials: 'same-origin',
      });
      if (!res.ok) throw new Error('Failed to delete');
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      toast({ title: 'User deleted', status: 'success', duration: 3000 });
      onDeleteClose();
    },
  });

  const handleDelete = (user: User) => {
    setDeleteTarget(user);
    onDeleteOpen();
  };

  if (isLoading) return <Text>Loading...</Text>;
  if (error) return <Text color="red.500">Failed to load users</Text>;

  const users = data?.users ?? [];

  return (
    <Box>
      <Flex justify="space-between" align="center" mb={6}>
        <Box>
          <Heading size="lg">Users</Heading>
          <Text color="gray.500" fontSize="sm" mt={1}>Manage user accounts and roles</Text>
        </Box>
        <Button colorScheme="blue" size="sm" onClick={onCreateOpen}>New User</Button>
      </Flex>

      <Card shadow="sm" borderRadius="lg">
        <CardBody p={0}>
          <Table size="sm">
            <Thead>
              <Tr>
                <Th>Username</Th>
                <Th>Role</Th>
                <Th>Actions</Th>
              </Tr>
            </Thead>
            <Tbody>
              {users.map((u) => (
                <Tr key={u.id}>
                  <Td fontWeight="medium">{u.username}</Td>
                  <Td>
                    <Select
                      size="xs"
                      w="120px"
                      value={u.role}
                      onChange={(e) => updateRoleMutation.mutate({ id: u.id, role: e.target.value })}
                    >
                      <option value="member">member</option>
                      <option value="approver">approver</option>
                      <option value="admin">admin</option>
                    </Select>
                  </Td>
                  <Td>
                    <Button size="xs" colorScheme="red" variant="ghost" onClick={() => handleDelete(u)}>
                      Delete
                    </Button>
                  </Td>
                </Tr>
              ))}
              {users.length === 0 && (
                <Tr><Td colSpan={3}><Text textAlign="center" color="gray.400" py={4}>No users</Text></Td></Tr>
              )}
            </Tbody>
          </Table>
        </CardBody>
      </Card>

      {/* Create User Modal */}
      <Modal isOpen={isCreateOpen} onClose={onCreateClose} size="md">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>Create User</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4}>
              <FormControl isRequired>
                <FormLabel fontSize="sm">Username</FormLabel>
                <Input
                  size="sm"
                  value={newUser.username}
                  onChange={(e) => setNewUser({ ...newUser, username: e.target.value })}
                />
              </FormControl>
              <FormControl isRequired>
                <FormLabel fontSize="sm">Password</FormLabel>
                <Input
                  size="sm"
                  type="password"
                  value={newUser.password}
                  onChange={(e) => setNewUser({ ...newUser, password: e.target.value })}
                />
              </FormControl>
              <FormControl>
                <FormLabel fontSize="sm">Role</FormLabel>
                <Select
                  size="sm"
                  value={newUser.role}
                  onChange={(e) => setNewUser({ ...newUser, role: e.target.value })}
                >
                  <option value="member">member</option>
                  <option value="approver">approver</option>
                  <option value="admin">admin</option>
                </Select>
              </FormControl>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button size="sm" variant="ghost" mr={3} onClick={onCreateClose}>Cancel</Button>
            <Button
              size="sm"
              colorScheme="blue"
              isLoading={createMutation.isPending}
              onClick={() => createMutation.mutate(newUser)}
              isDisabled={!newUser.username || !newUser.password}
            >
              Create
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* Delete Confirmation */}
      <AlertDialog isOpen={isDeleteOpen} leastDestructiveRef={cancelRef} onClose={onDeleteClose}>
        <AlertDialogOverlay>
          <AlertDialogContent>
            <AlertDialogHeader>Delete User</AlertDialogHeader>
            <AlertDialogBody>
              Are you sure you want to delete <strong>{deleteTarget?.username}</strong>? This cannot be undone.
            </AlertDialogBody>
            <AlertDialogFooter>
              <Button ref={cancelRef} size="sm" onClick={onDeleteClose}>Cancel</Button>
              <Button
                size="sm"
                colorScheme="red"
                ml={3}
                isLoading={deleteMutation.isPending}
                onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
              >
                Delete
              </Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialogOverlay>
      </AlertDialog>
    </Box>
  );
}
