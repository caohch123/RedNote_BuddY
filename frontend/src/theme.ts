import type { ThemeConfig } from 'antd';

export const workbenchPalette = {
  canvas: '#F6F4EF',
  surface: '#FAF9F6',
  paper: '#FFFDF9',
  inset: '#F0EDE6',
  ink: '#2E2C27',
  text: '#45413A',
  muted: '#777168',
  soft: '#8B8479',
  line: '#E3DED4',
  lineStrong: '#CFC7B9',
  accent: '#A5533D',
  accentHover: '#88432F',
  accentSoft: '#F1E7DD',
  sage: '#7A8B71',
  stone: '#B3A999',
  ochre: '#B58B57',
};

export const workbenchFonts = {
  body: 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", "PingFang SC", sans-serif',
  display: '"Iowan Old Style", "Palatino Linotype", "Book Antiqua", Georgia, "Noto Serif SC", "Source Han Serif SC", "Songti SC", SimSun, serif',
};

export function applyWorkbenchTheme() {
  for (const [key, value] of Object.entries(workbenchPalette)) document.documentElement.style.setProperty(`--wb-${key}`, value);
  document.documentElement.style.setProperty('--wb-font', workbenchFonts.body);
  document.documentElement.style.setProperty('--wb-display', workbenchFonts.display);
}

export const workbenchTheme: ThemeConfig = {
  token: {
    colorPrimary: workbenchPalette.accent,
    colorInfo: '#6E7B80',
    colorSuccess: '#607957',
    colorWarning: '#A17836',
    colorError: '#B34E40',
    colorBgLayout: workbenchPalette.canvas,
    colorBgContainer: workbenchPalette.paper,
    colorBgElevated: workbenchPalette.paper,
    colorText: workbenchPalette.text,
    colorTextHeading: workbenchPalette.ink,
    colorTextSecondary: workbenchPalette.muted,
    colorTextTertiary: workbenchPalette.soft,
    colorBorder: workbenchPalette.line,
    colorBorderSecondary: '#EBE6DD',
    colorFillAlter: workbenchPalette.surface,
    colorFillTertiary: workbenchPalette.inset,
    colorLink: workbenchPalette.accent,
    colorLinkHover: workbenchPalette.accentHover,
    borderRadius: 8,
    fontFamily: workbenchFonts.body,
    fontSize: 14,
    lineHeight: 1.65,
    boxShadow: '0 8px 32px #322B2010',
    boxShadowSecondary: '0 6px 24px #322B2012',
  },
  components: {
    Button: { colorPrimary: workbenchPalette.ink, colorPrimaryHover: '#48443D', colorPrimaryActive: '#211F1B', primaryShadow: 'none', defaultShadow: 'none', fontWeight: 500 },
    Menu: { itemBg: 'transparent', itemColor: workbenchPalette.muted, itemHoverBg: workbenchPalette.inset, itemSelectedBg: workbenchPalette.accentSoft, itemSelectedColor: '#7F4936', itemBorderRadius: 8 },
    Table: { headerBg: workbenchPalette.surface, headerColor: workbenchPalette.muted, rowHoverBg: '#F5F1E9', rowSelectedBg: workbenchPalette.accentSoft, borderColor: workbenchPalette.line },
    Tabs: { itemColor: workbenchPalette.muted, itemSelectedColor: workbenchPalette.accent, itemActiveColor: workbenchPalette.accent, inkBarColor: workbenchPalette.accent },
    Input: { activeShadow: '0 0 0 2px #A5533D0D', hoverBorderColor: workbenchPalette.lineStrong },
    Select: { activeOutlineColor: '#A5533D0D', optionSelectedBg: workbenchPalette.accentSoft },
    Card: { headerBg: 'transparent', headerFontSize: 15 },
    Segmented: { trackBg: workbenchPalette.inset, itemSelectedBg: workbenchPalette.paper, itemSelectedColor: workbenchPalette.ink },
  },
};
