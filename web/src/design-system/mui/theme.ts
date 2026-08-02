// Aura Power — tema Material UI.
// Regra de cor do sistema: o verde é a MARCA e o estado "em execução".
// A ação primária nasce na tinta (ink-900 no claro, ink-100 no escuro).
// A gramática de forma vem do logo "Janela": sólido = ligado, pontilhado = dormindo.

import { createTheme, alpha } from '@mui/material/styles';
import type { Theme, ThemeOptions } from '@mui/material/styles';
import { ramp, type, space, radius, motion, zIndex, light, dark, font } from '../tokens/tokens';

export type Mode = 'light' | 'dark';

declare module '@mui/material/styles' {
  interface Palette {
    brand: Palette['primary'];
    workload: {
      running: string; asleep: string; scheduled: string; failed: string; excluded: string;
    };
    surface: { canvas: string; raised: string; sunken: string; inset: string };
    dataViz: string[];
  }
  interface PaletteOptions {
    brand?: PaletteOptions['primary'];
    workload?: Palette['workload'];
    surface?: Palette['surface'];
    dataViz?: string[];
  }
  interface TypographyVariants {
    code: React.CSSProperties;
    metric: React.CSSProperties;
    metricSm: React.CSSProperties;
  }
  interface TypographyVariantsOptions {
    code?: React.CSSProperties;
    metric?: React.CSSProperties;
    metricSm?: React.CSSProperties;
  }
}
declare module '@mui/material/Typography' {
  interface TypographyPropsVariantOverrides {
    code: true; metric: true; metricSm: true;
  }
}

const px = (n: number) => `${n}px`;
const t = (k: keyof typeof type) => {
  const v = type[k];
  return {
    fontFamily: v.family === 'mono' ? font.family.mono : font.family.sans,
    fontSize: px(v.size),
    lineHeight: v.line,
    letterSpacing: `${v.tracking}em`,
    fontWeight: v.weight,
    ...(k === 'overline' ? { textTransform: 'uppercase' as const } : null),
  };
};

// MUI espera 25 posições no array de sombra.
const shadowScale = (m: Mode) => {
  const s = m === 'light' ? light.shadow : dark.shadow;
  const base = [s.none, s.xs, s.sm, s.sm, s.md, s.md, s.md, s.lg, s.lg, s.lg, s.lg, s.xl];
  return [...base, ...Array(25 - base.length).fill(s.xl)] as unknown as Theme['shadows'];
};

export function createAuraTheme(mode: Mode = 'light'): Theme {
  const c = (mode === 'light' ? light : dark).color;
  const sh = (mode === 'light' ? light.shadow : dark.shadow);
  const isLight = mode === 'light';

  const focus = {
    outline: 'none',
    boxShadow: `0 0 0 3px ${c.focusShadow}`,
    borderColor: c.focusRing,
  };

  const options: ThemeOptions = {
    // spacing(1) = 4px. Toda medida do sistema é múltipla de 4.
    spacing: 4,
    shape: { borderRadius: radius.md },
    zIndex,
    palette: {
      mode,
      common: { black: ramp.ink['950'], white: '#FFFFFF' },
      primary: {
        main: c.actionBg,
        dark: c.actionBgActive,
        light: c.actionBgHover,
        contrastText: c.actionFg,
      },
      secondary: {
        main: c.brandSolidBg, dark: c.brandSolidBgHover, light: c.brandMark,
        contrastText: c.brandSolidFg,
      },
      // `brand.main` é a cor do LOGO. Para gráfico funcional use `brand.light`
      // (= brand-mark, ≥3:1) e para texto use `palette.text` + token text-brand.
      brand: {
        main: c.brand, dark: c.brandSolidBg, light: c.brandMark,
        contrastText: c.brandSolidFg,
      },
      success: { main: c.successMark, light: c.successBg, dark: c.successFg, contrastText: '#FFFFFF' },
      warning: { main: c.warningMark, light: c.warningBg, dark: c.warningFg, contrastText: ramp.ink['950'] },
      error:   { main: c.dangerMark,  light: c.dangerBg,  dark: c.dangerFg,  contrastText: '#FFFFFF' },
      info:    { main: c.infoMark,    light: c.infoBg,    dark: c.infoFg,    contrastText: '#FFFFFF' },
      text: {
        primary: c.textPrimary,
        secondary: c.textSecondary,
        disabled: c.textDisabled,
      },
      background: { default: c.bgCanvas, paper: c.bgSurface },
      divider: c.borderDefault,
      surface: {
        canvas: c.bgCanvas, raised: c.bgSurfaceRaised,
        sunken: c.bgSurfaceSunken, inset: c.bgInset,
      },
      workload: {
        running: c.stateRunningMark, asleep: c.stateAsleepMark,
        scheduled: c.stateScheduledMark, failed: c.stateFailedMark,
        excluded: c.stateExcludedMark,
      },
      dataViz: [c.data1, c.data2, c.data3, c.data4, c.data5, c.data6],
      action: {
        hover: c.actionGhostBgHover,
        selected: isLight ? ramp.ink['100'] : 'rgb(255 255 255 / 0.10)',
        disabledBackground: c.actionBgDisabled,
        disabled: c.actionFgDisabled,
        focus: c.focusShadow,
      },
    },
    typography: {
      fontFamily: font.family.sans,
      fontWeightRegular: 400, fontWeightMedium: 500, fontWeightBold: 600,
      htmlFontSize: 16, fontSize: 15,
      h1: t('h1'), h2: t('h2'), h3: t('h3'), h4: t('h4'), h5: t('h5'), h6: t('h6'),
      subtitle1: t('subtitle1'), subtitle2: t('subtitle2'),
      body1: t('body1'), body2: t('body2'),
      button: { ...t('button'), textTransform: 'none' },
      caption: t('caption'), overline: t('overline'),
      code: t('code'), metric: t('metric'), metricSm: t('metricSm'),
    },
    shadows: shadowScale(mode),
    transitions: {
      duration: {
        shortest: motion.duration.fast,
        shorter: motion.duration.fast,
        short: motion.duration.base,
        standard: motion.duration.base,
        complex: motion.duration.slow,
        enteringScreen: motion.duration.slow,
        leavingScreen: motion.duration.base,
      },
      easing: {
        easeInOut: motion.easing.standard,
        easeOut: motion.easing.enter,
        easeIn: motion.easing.exit,
        sharp: motion.easing.emphasized,
      },
    },
    components: {
      MuiCssBaseline: {
        styleOverrides: {
          ':root': { colorScheme: mode },
          body: {
            backgroundColor: c.bgCanvas,
            color: c.textPrimary,
            WebkitFontSmoothing: 'antialiased',
            fontFeatureSettings: "'cv11', 'ss01'",
          },
          // números sempre tabulares — é software de infraestrutura
          'code, kbd, samp, pre, .ap-num': {
            fontFamily: font.family.mono,
            fontVariantNumeric: 'tabular-nums',
          },
          '::selection': { background: alpha(c.brand, 0.22) },
          '@media (prefers-reduced-motion: reduce)': {
            '*': { animationDuration: '0.01ms !important', transitionDuration: '0.01ms !important' },
          },
        },
      },

      MuiPaper: {
        defaultProps: { elevation: 0 },
        styleOverrides: {
          root: { backgroundImage: 'none', backgroundColor: c.bgSurface },
          outlined: { border: `1px solid ${c.borderDefault}` },
        },
      },

      // A elevação é a BORDA. Sombra só em overlay e em hover de destaque.
      MuiCard: {
        defaultProps: { elevation: 0, variant: 'outlined' },
        styleOverrides: {
          root: {
            borderRadius: radius.lg,
            border: `1px solid ${c.borderDefault}`,
            backgroundColor: c.bgSurface,
            transition: `border-color ${motion.duration.base}ms ${motion.easing.standard}, box-shadow ${motion.duration.base}ms ${motion.easing.standard}`,
          },
        },
      },
      MuiCardHeader: {
        styleOverrides: {
          root: { padding: `${space['5']}px ${space['5']}px ${space['2']}px` },
          title: t('h4'), subheader: { ...t('body2'), color: c.textSecondary },
        },
      },
      MuiCardContent: {
        styleOverrides: {
          root: { padding: space['5'], '&:last-child': { paddingBottom: space['5'] } },
        },
      },

      MuiButton: {
        defaultProps: { disableElevation: true, disableRipple: true },
        styleOverrides: {
          root: {
            borderRadius: radius.md,
            fontWeight: 500,
            minHeight: 36,
            padding: `0 ${space['4']}px`,
            gap: space['2'],
            transition: `background-color ${motion.duration.fast}ms ${motion.easing.standard}, border-color ${motion.duration.fast}ms ${motion.easing.standard}, color ${motion.duration.fast}ms ${motion.easing.standard}`,
            '&:focus-visible': focus,
            '& .MuiButton-startIcon > *:nth-of-type(1), & .MuiButton-endIcon > *:nth-of-type(1)': {
              fontSize: 18,
            },
          },
          sizeSmall: { minHeight: 30, padding: `0 ${space['3']}px`, fontSize: 13 },
          sizeLarge: { minHeight: 44, padding: `0 ${space['5']}px`, fontSize: 15 },
          contained: {
            backgroundColor: c.actionBg, color: c.actionFg,
            '&:hover': { backgroundColor: c.actionBgHover },
            '&:active': { backgroundColor: c.actionBgActive },
            '&.Mui-disabled': { backgroundColor: c.actionBgDisabled, color: c.actionFgDisabled },
          },
          outlined: {
            borderColor: c.actionOutlineBorder, color: c.actionOutlineFg,
            '&:hover': { borderColor: c.borderControlHover, backgroundColor: c.actionGhostBgHover },
            '&.Mui-disabled': { borderColor: c.borderDefault, color: c.actionFgDisabled },
          },
          text: {
            color: c.actionOutlineFg,
            '&:hover': { backgroundColor: c.actionGhostBgHover },
          },
          // Verde só quando a ação É a marca (ex.: "Ligar agora").
          containedSecondary: {
            backgroundColor: c.brandSolidBg, color: c.brandSolidFg,
            '&:hover': { backgroundColor: c.brandSolidBgHover },
          },
          containedError: { backgroundColor: c.dangerMark, color: '#FFFFFF' },
        },
      },

      MuiIconButton: {
        defaultProps: { disableRipple: true },
        styleOverrides: {
          root: {
            borderRadius: radius.sm,
            color: c.textSecondary,
            transition: `background-color ${motion.duration.fast}ms ${motion.easing.standard}`,
            '&:hover': { backgroundColor: c.actionGhostBgHover, color: c.textPrimary },
            '&:focus-visible': focus,
          },
          sizeSmall: { padding: space['1'] + 2 },
        },
      },

      MuiOutlinedInput: {
        styleOverrides: {
          root: {
            borderRadius: radius.md,
            backgroundColor: c.bgSurface,
            fontSize: 14,
            '& .MuiOutlinedInput-notchedOutline': { borderColor: c.borderControl },
            '&:hover .MuiOutlinedInput-notchedOutline': { borderColor: c.borderControlHover },
            '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
              borderColor: c.focusRing, borderWidth: 1,
            },
            '&.Mui-focused': { boxShadow: `0 0 0 3px ${c.focusShadow}` },
            '&.Mui-error.Mui-focused': { boxShadow: `0 0 0 3px ${alpha(c.dangerMark, 0.26)}` },
            '&.Mui-disabled': { backgroundColor: c.bgSurfaceSunken },
          },
          input: { padding: `${space['2'] + 1}px ${space['3']}px`, minHeight: 20 },
          inputSizeSmall: { padding: `${space['2']}px ${space['3']}px` },
        },
      },
      MuiInputLabel: {
        styleOverrides: {
          root: {
            ...t('subtitle2'), color: c.textSecondary,
            '&.Mui-focused': { color: c.textPrimary },
          },
        },
      },
      MuiFormHelperText: {
        styleOverrides: { root: { ...t('caption'), marginLeft: 0, marginTop: space['1'] + 2 } },
      },
      MuiTextField: { defaultProps: { size: 'small', variant: 'outlined' } },

      MuiSelect: { styleOverrides: { select: { fontSize: 14 } } },
      MuiMenu: {
        styleOverrides: {
          paper: {
            borderRadius: radius.md, border: `1px solid ${c.borderDefault}`,
            boxShadow: sh.lg, backgroundColor: c.bgOverlay, marginTop: space['1'],
          },
          list: { padding: space['1'] },
        },
      },
      MuiMenuItem: {
        styleOverrides: {
          root: {
            borderRadius: radius.xs, fontSize: 14, minHeight: 32,
            padding: `${space['2']}px ${space['2']}px`,
            '&.Mui-selected': { backgroundColor: c.actionGhostBgHover },
          },
        },
      },

      // Chip = o portador do vocabulário de estado. Nunca só cor: sempre cor + rótulo + marca.
      MuiChip: {
        styleOverrides: {
          root: {
            borderRadius: radius.full, height: 24, fontSize: 12, fontWeight: 500,
            fontFamily: font.family.sans, letterSpacing: 0,
          },
          label: { padding: `0 ${space['2']}px` },
          sizeSmall: { height: 20, fontSize: 11 },
          outlined: { borderColor: c.borderDefault },
        },
      },

      MuiAlert: {
        defaultProps: { variant: 'outlined' },
        styleOverrides: {
          root: { borderRadius: radius.md, padding: `${space['3']}px ${space['4']}px`, fontSize: 13.5 },
          standardSuccess: { backgroundColor: c.successBg, color: c.successFg },
          standardWarning: { backgroundColor: c.warningBg, color: c.warningFg },
          standardError: { backgroundColor: c.dangerBg, color: c.dangerFg },
          standardInfo: { backgroundColor: c.infoBg, color: c.infoFg },
          outlinedSuccess: { backgroundColor: c.successBg, borderColor: c.successBorder, color: c.successFg },
          outlinedWarning: { backgroundColor: c.warningBg, borderColor: c.warningBorder, color: c.warningFg },
          outlinedError: { backgroundColor: c.dangerBg, borderColor: c.dangerBorder, color: c.dangerFg },
          outlinedInfo: { backgroundColor: c.infoBg, borderColor: c.infoBorder, color: c.infoFg },
          icon: { padding: 0, marginRight: space['3'], alignItems: 'center' },
          message: { padding: 0 },
        },
      },

      MuiTableContainer: {
        styleOverrides: {
          root: { border: `1px solid ${c.borderDefault}`, borderRadius: radius.lg },
        },
      },
      MuiTableCell: {
        styleOverrides: {
          root: {
            borderBottom: `1px solid ${c.borderSubtle}`,
            padding: `${space['3']}px ${space['4']}px`, fontSize: 13.5,
          },
          head: {
            ...t('overline'), color: c.textSecondary,
            backgroundColor: c.bgSurfaceSunken,
            borderBottom: `1px solid ${c.borderDefault}`,
            paddingTop: space['2'] + 2, paddingBottom: space['2'] + 2,
          },
        },
      },
      MuiTableRow: {
        styleOverrides: {
          root: {
            transition: `background-color ${motion.duration.fast}ms ${motion.easing.standard}`,
            '&:last-child td': { borderBottom: 0 },
            '&.MuiTableRow-hover:hover': { backgroundColor: c.actionGhostBgHover },
          },
        },
      },

      MuiTabs: {
        styleOverrides: {
          root: { minHeight: 40, borderBottom: `1px solid ${c.borderDefault}` },
          indicator: { height: 2, backgroundColor: c.brandMark, borderRadius: 2 },
        },
      },
      MuiTab: {
        defaultProps: { disableRipple: true },
        styleOverrides: {
          root: {
            minHeight: 40, padding: `0 ${space['3']}px`, marginRight: space['4'],
            fontSize: 14, fontWeight: 500, textTransform: 'none', color: c.textSecondary,
            minWidth: 0,
            '&.Mui-selected': { color: c.textPrimary },
            '&:focus-visible': { ...focus, borderRadius: radius.xs },
          },
        },
      },

      MuiTooltip: {
        defaultProps: { arrow: false, enterDelay: 320 },
        styleOverrides: {
          tooltip: {
            backgroundColor: isLight ? ramp.ink['900'] : ramp.ink['100'],
            color: isLight ? '#FFFFFF' : ramp.ink['950'],
            fontSize: 12, fontWeight: 400, lineHeight: 1.45,
            padding: `${space['2']}px ${space['3']}px`, borderRadius: radius.sm,
            maxWidth: 280,
          },
        },
      },

      MuiDialog: {
        styleOverrides: {
          paper: {
            borderRadius: radius.xl, border: `1px solid ${c.borderDefault}`,
            boxShadow: sh.xl, backgroundColor: c.bgOverlay, backgroundImage: 'none',
          },
        },
      },
      MuiDialogTitle: { styleOverrides: { root: { ...t('h3'), padding: `${space['6']}px ${space['6']}px ${space['2']}px` } } },
      MuiDialogContent: { styleOverrides: { root: { padding: `0 ${space['6']}px`, fontSize: 14 } } },
      MuiDialogActions: { styleOverrides: { root: { padding: space['6'], gap: space['2'] } } },
      MuiBackdrop: { styleOverrides: { root: { backgroundColor: c.bgScrim } } },

      // O switch é O controle deste produto: ligar/desligar um workload.
      MuiSwitch: {
        styleOverrides: {
          root: { width: 38, height: 22, padding: 0, overflow: 'visible' },
          switchBase: {
            padding: 3,
            '&.Mui-checked': {
              transform: 'translateX(16px)', color: '#FFFFFF',
              '& + .MuiSwitch-track': { backgroundColor: c.brandMark, opacity: 1 },
            },
            '&.Mui-focusVisible + .MuiSwitch-track': { boxShadow: `0 0 0 3px ${c.focusShadow}` },
            '&.Mui-disabled + .MuiSwitch-track': { opacity: 0.4 },
          },
          thumb: { width: 16, height: 16, boxShadow: sh.xs, backgroundColor: '#FFFFFF' },
          track: {
            borderRadius: radius.full, backgroundColor: isLight ? ramp.ink['300'] : ramp.ink['600'],
            opacity: 1,
            transition: `background-color ${motion.duration.base}ms ${motion.easing.standard}`,
          },
        },
      },
      MuiCheckbox: { defaultProps: { disableRipple: true }, styleOverrides: { root: { color: c.borderControl, '&.Mui-checked': { color: c.actionBg } } } },
      MuiRadio: { defaultProps: { disableRipple: true }, styleOverrides: { root: { color: c.borderControl, '&.Mui-checked': { color: c.actionBg } } } },

      MuiLinearProgress: {
        styleOverrides: {
          root: { height: 4, borderRadius: radius.full, backgroundColor: c.bgSurfaceSunken },
          bar: { borderRadius: radius.full, backgroundColor: c.brandMark },
        },
      },
      MuiCircularProgress: { defaultProps: { thickness: 4 }, styleOverrides: { root: { color: c.brandMark } } },
      MuiDivider: { styleOverrides: { root: { borderColor: c.borderDefault } } },
      MuiSkeleton: {
        defaultProps: { animation: 'wave' },
        styleOverrides: { root: { backgroundColor: c.bgSurfaceSunken, borderRadius: radius.xs } },
      },
      MuiLink: {
        defaultProps: { underline: 'hover' },
        styleOverrides: {
          root: {
            color: c.textLink, textDecorationColor: c.borderStrong, textUnderlineOffset: 3,
            '&:hover': { color: c.brandFg, textDecorationColor: 'currentColor' },
            '&:focus-visible': { ...focus, borderRadius: 2 },
          },
        },
      },
      MuiAppBar: {
        defaultProps: { elevation: 0, color: 'inherit' },
        styleOverrides: {
          root: {
            backgroundColor: c.bgSurface, color: c.textPrimary,
            borderBottom: `1px solid ${c.borderDefault}`, backgroundImage: 'none',
          },
        },
      },
      MuiDrawer: {
        styleOverrides: {
          paper: { backgroundColor: c.bgSurface, borderColor: c.borderDefault, backgroundImage: 'none' },
        },
      },
      MuiListItemButton: {
        defaultProps: { disableRipple: true },
        styleOverrides: {
          root: {
            borderRadius: radius.sm, minHeight: 34, fontSize: 14,
            '&.Mui-selected': {
              backgroundColor: c.actionGhostBgHover, color: c.textPrimary, fontWeight: 500,
            },
            '&:focus-visible': focus,
          },
        },
      },
      MuiToggleButton: {
        styleOverrides: {
          root: {
            textTransform: 'none', fontSize: 13, fontWeight: 500, borderRadius: radius.md,
            borderColor: c.borderDefault, color: c.textSecondary, padding: `${space['1']}px ${space['3']}px`,
            '&.Mui-selected': { backgroundColor: c.actionGhostBgHover, color: c.textPrimary },
          },
        },
      },
    },
  };

  return createTheme(options);
}

export const auraLight = createAuraTheme('light');
export const auraDark = createAuraTheme('dark');
