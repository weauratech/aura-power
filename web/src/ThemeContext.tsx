import React, { createContext, useContext, useMemo, useState, useEffect } from 'react';
import { ThemeProvider, CssBaseline } from '@mui/material';
import useMediaQuery from '@mui/material/useMediaQuery';
import { createAuraTheme, type Mode } from './design-system/mui';

interface ThemeContextValue {
  mode: Mode;
  toggleMode: () => void;
}

const ThemeCtx = createContext<ThemeContextValue>({ mode: 'light', toggleMode: () => {} });

export function useThemeMode() {
  return useContext(ThemeCtx);
}

export function ThemeContextProvider({ children }: { children: React.ReactNode }) {
  const prefersDark = useMediaQuery('(prefers-color-scheme: dark)');
  const [mode, setMode] = useState<Mode>(() => {
    const stored = localStorage.getItem('aura-power-theme') as Mode | null;
    return stored ?? (prefersDark ? 'dark' : 'light');
  });

  useEffect(() => {
    localStorage.setItem('aura-power-theme', mode);
  }, [mode]);

  const toggleMode = () => setMode(prev => (prev === 'light' ? 'dark' : 'light'));

  const theme = useMemo(() => createAuraTheme(mode), [mode]);

  return (
    <ThemeCtx.Provider value={{ mode, toggleMode }}>
      <ThemeProvider theme={theme}>
        <CssBaseline />
        {children}
      </ThemeProvider>
    </ThemeCtx.Provider>
  );
}
