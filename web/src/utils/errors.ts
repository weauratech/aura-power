/**
 * Maps raw API error messages to user-friendly display messages.
 */
const ERROR_MAP: Record<string, string> = {
  // Auth
  'invalid credentials': 'Username or password is incorrect. Please try again.',
  'username and password required': 'Please fill in both username and password.',
  'missing authorization': 'Your session has expired. Please sign in again.',
  'invalid or expired token': 'Your session has expired. Please sign in again.',
  'insufficient permissions': 'You do not have permission to perform this action.',
  'refreshToken required': 'Session could not be renewed. Please sign in again.',
  'failed to generate token': 'An internal error occurred. Please try again later.',
  'user not found': 'User account not found.',
  'invalid refresh token': 'Session expired. Please sign in again.',

  // Users
  'username already exists': 'A user with that username already exists.',
  'role must be member, approver, or admin': 'Invalid role. Choose member, approver, or admin.',

  // Policies & Overrides
  'invalid policy': 'The policy data is invalid. Please check the form fields.',
  'invalid override': 'The override data is invalid. Please check the form fields.',
  'invalid policy spec': 'The policy specification is invalid.',
  'invalid override spec': 'The override specification is invalid.',
  'invalid group': 'The namespace group data is invalid.',

  // Resources
  'target not found': 'Target workload not found.',
  'metrics provider not available': 'Metrics are not available. Prometheus may not be configured.',
  'cost provider not available': 'Cost data is not available. OpenCost may not be configured.',

  // Generic
  'Connection failed': 'Unable to reach the server. Check your connection.',
};

/**
 * Translates a raw API error message into a user-friendly string.
 * Falls back to a formatted version of the original if no mapping exists.
 */
export function friendlyError(raw: string): string {
  // Direct match
  const lower = raw.toLowerCase();
  for (const [key, friendly] of Object.entries(ERROR_MAP)) {
    if (lower === key.toLowerCase()) return friendly;
  }

  // Partial match (for messages that contain a known pattern)
  if (lower.includes('failed to create')) return 'Failed to create the resource. It may already exist.';
  if (lower.includes('failed to delete')) return 'Failed to delete the resource. It may have already been removed.';
  if (lower.includes('failed to load')) return 'Failed to load data from the server.';
  if (lower.includes('not found')) return 'The requested resource was not found.';
  if (lower.includes('already exists')) return 'A resource with that name already exists.';
  if (lower.includes('timeout')) return 'The request timed out. Please try again.';

  // Fallback: capitalize first letter
  return raw.charAt(0).toUpperCase() + raw.slice(1);
}
