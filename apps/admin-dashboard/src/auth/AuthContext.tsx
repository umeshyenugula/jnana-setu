import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  clearSession,
  loadSession,
  saveSession,
  signIn as doSignIn,
  signOut as doSignOut,
  isExpired,
} from "./supabaseAuth";
import type { AuthSession } from "./supabaseAuth";

interface AuthState {
  session: AuthSession | null;
  signIn: (email: string, password: string) => Promise<void>;
  signOut: () => Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<AuthSession | null>(() => loadSession());

  useEffect(() => {
    if (session && isExpired(session)) {
      clearSession();
      setSession(null);
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const signIn = useCallback(async (email: string, password: string) => {
    const next = await doSignIn(email, password);
    saveSession(next);
    setSession(next);
  }, []);

  const signOut = useCallback(async () => {
    await doSignOut(loadSession());
    clearSession();
    setSession(null);
  }, []);

  const value = useMemo(() => ({ session, signIn, signOut }), [session, signIn, signOut]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const state = useContext(AuthContext);
  if (!state) throw new Error("useAuth must be used inside AuthProvider.");
  return state;
}
