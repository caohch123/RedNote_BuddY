import { createContext, useContext } from 'react';

export interface Account {
  id: string;
  username: string;
  display_name: string;
  role: 'admin' | 'member';
  enabled: boolean;
  must_change_password: boolean;
  created_at: string;
  daily_request_limit: number;
  max_concurrent: number;
}

export type Realm = 'user' | 'admin';
export interface AuthState {
  user: Account;
  realm: Realm;
  logout: () => Promise<void>;
  changePassword: (currentPassword: string, newPassword: string) => Promise<void>;
}
export const AuthContext = createContext<AuthState | undefined>(undefined);
export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error('账户会话尚未初始化');
  return value;
}
