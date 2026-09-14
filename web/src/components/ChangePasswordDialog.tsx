import { useEffect, useState, type FormEvent } from 'react';
import Alert from '@mui/material/Alert';
import Button from '@mui/material/Button';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogContentText from '@mui/material/DialogContentText';
import DialogTitle from '@mui/material/DialogTitle';
import Stack from '@mui/material/Stack';
import TextField from '@mui/material/TextField';
import { apiPut } from '../hooks/useApi';

const POLICY = 'Use at least 12 characters and no more than 72 UTF-8 bytes. Common and repetitive passwords are rejected.';

export interface ChangePasswordDialogProps {
  open: boolean;
  onClose: () => void;
  onChanged: () => Promise<void> | void;
}

export function ChangePasswordDialog({ open, onClose, onChanged }: ChangePasswordDialogProps) {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) {
      setCurrentPassword('');
      setNewPassword('');
      setConfirmation('');
      setError('');
      setSubmitting(false);
    }
  }, [open]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError('');
    if (newPassword !== confirmation) {
      setError('New password and confirmation do not match.');
      return;
    }
    const byteLength = new TextEncoder().encode(newPassword).length;
    if ([...newPassword].length < 12 || byteLength > 72) {
      setError(POLICY);
      return;
    }
    setSubmitting(true);
    try {
      await apiPut('/auth/password', { currentPassword, newPassword }, { redirectOnUnauthorized: false });
      await onChanged();
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Password change failed. Please try again.');
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onClose={submitting ? undefined : onClose} fullWidth maxWidth="xs" aria-describedby="change-password-policy">
      <DialogTitle>Change password</DialogTitle>
      <DialogContent>
        <DialogContentText id="change-password-policy" sx={{ mb: 2 }}>{POLICY}</DialogContentText>
        <Stack component="form" id="change-password-form" spacing={2} onSubmit={submit}>
          {error && <Alert severity="error" role="alert">{error}</Alert>}
          <TextField
            autoFocus required fullWidth type="password" autoComplete="current-password"
            label="Current password" value={currentPassword}
            onChange={(event) => setCurrentPassword(event.target.value)} disabled={submitting}
          />
          <TextField
            required fullWidth type="password" autoComplete="new-password"
            label="New password" value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)} disabled={submitting}
          />
          <TextField
            required fullWidth type="password" autoComplete="new-password"
            label="Confirm new password" value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)} disabled={submitting}
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>Cancel</Button>
        <Button type="submit" form="change-password-form" variant="contained" disabled={submitting}>
          {submitting ? 'Changing password…' : 'Change password'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
