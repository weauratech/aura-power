import { createContext } from 'react';

export type CurrentUser = { id: string; username: string; role: 'member' | 'approver' | 'admin' };
export const CurrentUserContext = createContext<CurrentUser | null>(null);
