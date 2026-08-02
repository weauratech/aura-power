import { useState } from 'react';
import { Link, Outlet, useLocation } from 'react-router-dom';
import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';
import AppBar from '@mui/material/AppBar';
import Toolbar from '@mui/material/Toolbar';
import Typography from '@mui/material/Typography';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemIcon from '@mui/material/ListItemIcon';
import ListItemText from '@mui/material/ListItemText';
import Divider from '@mui/material/Divider';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import Stack from '@mui/material/Stack';
import Chip from '@mui/material/Chip';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';
import DashboardIcon from '@mui/icons-material/DashboardOutlined';
import DevicesIcon from '@mui/icons-material/DevicesOutlined';
import PolicyIcon from '@mui/icons-material/PolicyOutlined';
import ScheduleIcon from '@mui/icons-material/ScheduleOutlined';
import BarChartIcon from '@mui/icons-material/BarChartOutlined';
import BlockIcon from '@mui/icons-material/BlockOutlined';
import SavingsIcon from '@mui/icons-material/SavingsOutlined';
import PendingIcon from '@mui/icons-material/HourglassEmptyOutlined';
import PeopleIcon from '@mui/icons-material/PeopleOutlined';
import MenuIcon from '@mui/icons-material/MenuOutlined';
import LightModeIcon from '@mui/icons-material/LightModeOutlined';
import DarkModeIcon from '@mui/icons-material/DarkModeOutlined';
import LogoutIcon from '@mui/icons-material/LogoutOutlined';
import { useThemeMode } from '../ThemeContext';

const DRAWER_WIDTH = 240;

interface NavItem {
  path: string;
  label: string;
  icon: React.ReactNode;
}

interface LayoutProps {
  user?: { username: string; role: string } | null;
  onLogout?: () => void;
}

const NAV_ITEMS: NavItem[] = [
  { path: '/', label: 'Dashboard', icon: <DashboardIcon /> },
  { path: '/targets', label: 'Targets', icon: <DevicesIcon /> },
  { path: '/policies', label: 'Policies', icon: <PolicyIcon /> },
  { path: '/schedule', label: 'Schedule', icon: <ScheduleIcon /> },
  { path: '/metrics', label: 'Metrics', icon: <BarChartIcon /> },
  { path: '/blocked', label: 'Blocked', icon: <BlockIcon /> },
  { path: '/savings', label: 'Savings', icon: <SavingsIcon /> },
];

function isActive(currentPath: string, itemPath: string): boolean {
  if (itemPath === '/') return currentPath === '/';
  return currentPath.startsWith(itemPath);
}

export function Layout({ user, onLogout }: LayoutProps) {
  const location = useLocation();
  const { mode, toggleMode } = useThemeMode();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('md'));
  const [mobileOpen, setMobileOpen] = useState(false);

  const showPending = user && (user.role === 'approver' || user.role === 'admin');
  const showUsers = user && user.role === 'admin';

  const drawerContent = (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Stack direction="row" alignItems="center" spacing={2} sx={{ px: 3, py: 3 }}>
        <Box
          component="img"
          src={new URL('../design-system/assets/logo/aura-power-lockup-dark.svg', import.meta.url).href}
          alt="Aura Power"
          sx={{ height: 28 }}
        />
      </Stack>

      <List sx={{ flex: 1, px: 1.5 }}>
        {NAV_ITEMS.map((item) => {
          const active = isActive(location.pathname, item.path);
          return (
            <ListItemButton
              key={item.path}
              component={Link}
              to={item.path}
              selected={active}
              onClick={() => isMobile && setMobileOpen(false)}
              sx={{ mb: 0.5, py: 1 }}
            >
              <ListItemIcon sx={{ minWidth: 36, color: active ? 'text.primary' : 'text.secondary' }}>
                {item.icon}
              </ListItemIcon>
              <ListItemText primary={item.label} primaryTypographyProps={{ variant: 'body2', sx: { fontWeight: active ? 500 : 400 } }} />
            </ListItemButton>
          );
        })}

        {showPending && (
          <>
            <Divider sx={{ my: 1.5 }} />
            <ListItemButton
              component={Link}
              to="/pending"
              selected={isActive(location.pathname, '/pending')}
              onClick={() => isMobile && setMobileOpen(false)}
              sx={{ py: 1 }}
            >
              <ListItemIcon sx={{ minWidth: 36 }}><PendingIcon /></ListItemIcon>
              <ListItemText primary="Pending" primaryTypographyProps={{ variant: 'body2' }} />
            </ListItemButton>
          </>
        )}

        {showUsers && (
          <ListItemButton
            component={Link}
            to="/users"
            selected={isActive(location.pathname, '/users')}
            onClick={() => isMobile && setMobileOpen(false)}
            sx={{ py: 1 }}
          >
            <ListItemIcon sx={{ minWidth: 36 }}><PeopleIcon /></ListItemIcon>
            <ListItemText primary="Users" primaryTypographyProps={{ variant: 'body2' }} />
          </ListItemButton>
        )}
      </List>

      <Box sx={{ px: 2, py: 2, borderTop: 1, borderColor: 'divider' }}>
        {user && (
          <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 1 }}>
            <Box>
              <Typography variant="body2" sx={{ fontWeight: 500 }}>{user.username}</Typography>
              <Chip label={user.role} size="small" sx={{ mt: 0.5, height: 20, fontSize: 11 }} />
            </Box>
            <Tooltip title="Sign out">
              <IconButton size="small" onClick={onLogout}>
                <LogoutIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          </Stack>
        )}
        <Typography variant="caption" color="text.disabled">Aura Power v2.0</Typography>
      </Box>
    </Box>
  );

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh' }}>
      {/* Mobile AppBar */}
      {isMobile && (
        <AppBar position="fixed" sx={{ zIndex: theme.zIndex.drawer + 1 }}>
          <Toolbar sx={{ minHeight: 56 }}>
            <IconButton edge="start" onClick={() => setMobileOpen(true)} sx={{ mr: 2 }}>
              <MenuIcon />
            </IconButton>
            <Box
              component="img"
              src={new URL('../design-system/assets/logo/aura-power-lockup-dark.svg', import.meta.url).href}
              alt="Aura Power"
              sx={{ height: 22 }}
            />
            <Box sx={{ flex: 1 }} />
            <IconButton onClick={toggleMode} size="small">
              {mode === 'dark' ? <LightModeIcon /> : <DarkModeIcon />}
            </IconButton>
          </Toolbar>
        </AppBar>
      )}

      {/* Sidebar */}
      <Drawer
        variant={isMobile ? 'temporary' : 'permanent'}
        open={isMobile ? mobileOpen : true}
        onClose={() => setMobileOpen(false)}
        sx={{
          width: DRAWER_WIDTH,
          flexShrink: 0,
          '& .MuiDrawer-paper': { width: DRAWER_WIDTH, boxSizing: 'border-box' },
        }}
      >
        {drawerContent}
      </Drawer>

      {/* Main content */}
      <Box
        component="main"
        sx={{
          flexGrow: 1,
          bgcolor: 'background.default',
          minHeight: '100vh',
          mt: isMobile ? '56px' : 0,
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        {/* Desktop top bar — just the theme toggle */}
        {!isMobile && (
          <Box sx={{ display: 'flex', justifyContent: 'flex-end', px: 3, py: 1.5, borderBottom: 1, borderColor: 'divider' }}>
            <IconButton onClick={toggleMode} size="small">
              {mode === 'dark' ? <LightModeIcon fontSize="small" /> : <DarkModeIcon fontSize="small" />}
            </IconButton>
          </Box>
        )}
        <Box sx={{ p: { xs: 3, md: 5 }, flex: 1 }}>
          <Outlet />
        </Box>
      </Box>
    </Box>
  );
}
