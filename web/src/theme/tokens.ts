import { theme, type ThemeConfig } from 'antd'

// Design tokens from design/README.md (Claude Design handoff, 2026-09-13).
export const AMBER = '#E8963A'
export const INK = '#1E2A3B'

export const fontFamily =
  '-apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans TC", "Noto Sans SC", "Noto Sans JP", "Noto Sans KR", sans-serif'
export const fontFamilyCode = '"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace'

export const light: ThemeConfig = {
  algorithm: theme.defaultAlgorithm,
  token: {
    fontFamily, fontFamilyCode,
    fontSize: 14, fontSizeSM: 12, fontSizeLG: 16,
    borderRadiusSM: 4, borderRadius: 6, borderRadiusLG: 10,
    colorPrimary: INK, colorLink: '#35558A', colorLinkHover: AMBER,
    colorBgLayout: '#F6F4EF', colorBgContainer: '#FFFFFF', colorBgElevated: '#FFFFFF',
    colorText: INK, colorTextSecondary: '#5B6676', colorTextTertiary: '#8A94A3', colorTextQuaternary: '#C9C4B9',
    colorBorder: '#D5D0C6', colorBorderSecondary: '#E3DFD6', colorSplit: '#F1EFEA',
    colorSuccess: '#2E9E6B', colorWarning: '#D4A017', colorError: '#D14343', colorInfo: '#2B57A8',
    colorFillAlter: '#FBFAF7', colorFillSecondary: '#F1EFEA', colorFillTertiary: '#F6F4EF', colorFillQuaternary: '#FBFAF7',
    boxShadow: '0 8px 28px rgba(19,25,35,.14)', boxShadowSecondary: '0 8px 28px rgba(19,25,35,.14)',
    controlHeight: 36, controlHeightSM: 28, controlHeightLG: 42,
  },
  components: {
    Layout: { siderBg: '#F6F4EF', headerBg: '#FFFFFF', bodyBg: '#F6F4EF', headerHeight: 52, headerPadding: '0 20px' },
    Menu: { itemBg: 'transparent', itemSelectedBg: '#ECE8E0', itemSelectedColor: INK, itemHeight: 32, itemBorderRadius: 6, itemMarginInline: 0, groupTitleFontSize: 10, iconSize: 14, collapsedIconSize: 16 },
    Table: { headerBg: '#FBFAF7', headerColor: '#8A94A3', rowHoverBg: '#FBFAF7', cellPaddingBlock: 10, cellPaddingInline: 16, headerSplitColor: 'transparent', borderColor: '#F1EFEA' },
    Button: { primaryColor: '#F3EFE7', defaultBg: '#FFFFFF', defaultHoverBg: '#F6F4EF', fontWeight: 500 },
    Card: { paddingLG: 20 },
    Drawer: { paddingLG: 24 },
    Modal: { borderRadiusLG: 12 },
    Tag: { borderRadiusSM: 4 },
    Alert: { colorInfoBg: '#EEF2F9', colorInfoBorder: '#D6E0F2', colorWarningBg: '#FBF3DC', colorWarningBorder: '#EFD98E', colorErrorBg: '#FBEAEA', colorErrorBorder: '#F0C9C9' },
    Tabs: { inkBarColor: INK, itemSelectedColor: INK },
    Steps: { colorPrimary: INK },
    Segmented: { itemSelectedBg: INK, itemSelectedColor: '#F3EFE7' },
  },
}

export const dark: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: {
    fontFamily, fontFamilyCode,
    fontSize: 14, fontSizeSM: 12, fontSizeLG: 16,
    borderRadiusSM: 4, borderRadius: 6, borderRadiusLG: 10,
    colorPrimary: '#E9E6DF', colorLink: '#8FB0E0', colorLinkHover: AMBER,
    colorBgLayout: '#131923', colorBgContainer: '#1B2331', colorBgElevated: '#222D3D', colorBgSpotlight: '#222D3D',
    colorText: '#E9E6DF', colorTextSecondary: '#9AA5B5', colorTextTertiary: '#6F7A8A', colorTextQuaternary: '#4B5563',
    colorBorder: '#3A4658', colorBorderSecondary: '#2C3747', colorSplit: '#222D3D',
    colorSuccess: '#4DBA86', colorWarning: '#E3B93A', colorError: '#E56B6B', colorInfo: '#7FA3D6',
    colorFillAlter: '#161D29', colorFillSecondary: '#222D3D', colorFillTertiary: '#1F2937', colorFillQuaternary: '#161D29',
    boxShadow: '0 8px 28px rgba(0,0,0,.4)', boxShadowSecondary: '0 8px 28px rgba(0,0,0,.4)',
    controlHeight: 36, controlHeightSM: 28, controlHeightLG: 42,
  },
  components: {
    Layout: { siderBg: '#131923', headerBg: '#1B2331', bodyBg: '#131923', headerHeight: 52, headerPadding: '0 20px' },
    Menu: { itemBg: 'transparent', itemSelectedBg: '#222D3D', itemSelectedColor: '#F3EFE7', itemHeight: 32, itemBorderRadius: 6, itemMarginInline: 0, groupTitleFontSize: 10, iconSize: 14, collapsedIconSize: 16 },
    Table: { headerBg: '#161D29', headerColor: '#6F7A8A', rowHoverBg: '#1F2937', cellPaddingBlock: 10, cellPaddingInline: 16, headerSplitColor: 'transparent', borderColor: '#222D3D' },
    Button: { primaryColor: '#131923', defaultBg: '#1B2331', defaultHoverBg: '#222D3D', fontWeight: 500 },
    Card: { paddingLG: 20 },
    Drawer: { paddingLG: 24 },
    Modal: { borderRadiusLG: 12 },
    Tag: { borderRadiusSM: 4 },
    Tabs: { inkBarColor: AMBER, itemSelectedColor: '#F3EFE7' },
    Steps: { colorPrimary: '#E9E6DF' },
    Segmented: { itemSelectedBg: '#E9E6DF', itemSelectedColor: '#131923' },
    // colorPrimary is a pale off-white here, which reads fine as a button
    // fill but turns an "on" switch into a grey pill that looks disabled.
    // A switch has to say on or off at a glance, so it uses the accent.
    Switch: { colorPrimary: AMBER, colorPrimaryHover: '#F0A855' },
  },
}

export const typeTag = {
  light: {
    hosted: { bg: '#E8EEF9', fg: '#2B57A8', border: '#C9D7F0' },
    proxy: { bg: '#F0EAFA', fg: '#6B44B3', border: '#DCCFF2' },
    group: { bg: '#E6F4EC', fg: '#1F7A4D', border: '#C3E3D1' },
    offline: { bg: '#F1EFEA', fg: '#5B6676', border: '#E3DFD6' },
    blocked: { bg: '#FBEAEA', fg: '#A83232', border: '#F0C9C9' },
  },
  dark: {
    hosted: { bg: 'rgba(127,163,214,.15)', fg: '#8FB0E0', border: 'rgba(127,163,214,.35)' },
    proxy: { bg: 'rgba(160,120,220,.15)', fg: '#B79CE8', border: 'rgba(160,120,220,.35)' },
    group: { bg: 'rgba(77,186,134,.15)', fg: '#4DBA86', border: 'rgba(77,186,134,.35)' },
    offline: { bg: '#222D3D', fg: '#9AA5B5', border: '#3A4658' },
    blocked: { bg: 'rgba(229,107,107,.15)', fg: '#E56B6B', border: 'rgba(229,107,107,.35)' },
  },
}

// Format icons: abbreviation + ecosystem colour (no third-party logos).
export const formatIcons: Record<string, { abbr: string; color: string; fg?: string; label: string }> = {
  maven: { abbr: 'mvn', color: '#B8541F', label: 'Maven' },
  npm: { abbr: 'npm', color: '#CB3837', label: 'npm' },
  docker: { abbr: 'dkr', color: '#1D63ED', label: 'Docker / OCI' },
  pypi: { abbr: 'py', color: '#3572A5', label: 'PyPI' },
  raw: { abbr: 'raw', color: '#6B7280', label: 'Raw' },
  nuget: { abbr: 'nu', color: '#004880', label: 'NuGet' },
  helm: { abbr: 'helm', color: '#0F1689', label: 'Helm' },
  go: { abbr: 'go', color: '#00879E', label: 'Go' },
  apt: { abbr: 'apt', color: '#A80030', label: 'APT' },
  yum: { abbr: 'yum', color: '#B22222', label: 'YUM' },
  alpine: { abbr: 'apk', color: '#0D597F', label: 'Alpine' },
  rubygems: { abbr: 'gem', color: '#CC342D', label: 'RubyGems' },
  cargo: { abbr: 'crg', color: '#B7410E', label: 'Cargo' },
  composer: { abbr: 'php', color: '#7377AD', label: 'Composer' },
  conda: { abbr: 'cnd', color: '#3E9A2A', label: 'Conda' },
  r: { abbr: 'R', color: '#276DC3', label: 'CRAN (R)' },
  p2: { abbr: 'p2', color: '#2C2255', label: 'p2' },
  cocoapods: { abbr: 'pod', color: '#EE3322', label: 'CocoaPods' },
  terraform: { abbr: 'tf', color: '#7B42BC', label: 'Terraform' },
  pub: { abbr: 'pub', color: '#0175C2', label: 'pub (Dart)' },
  gitlfs: { abbr: 'lfs', color: '#F05032', label: 'Git LFS' },
  huggingface: { abbr: 'hf', color: '#FFD21E', fg: '#1E2A3B', label: 'Hugging Face' },
  ansiblegalaxy: { abbr: 'ans', color: '#C8102E', label: 'Ansible Galaxy' },
  conan: { abbr: 'con', color: '#6699CB', label: 'Conan' },
  swift: { abbr: 'swf', color: '#F05138', label: 'Swift' },
}
export const formatInfo = (f: string) => formatIcons[f] ?? { abbr: f.slice(0, 3), color: '#6B7280', label: f }
