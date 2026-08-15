import type {AppTheme, AppThemeStore} from "@/types"

export type AppThemeMode = "light" | "dark"

export type AppThemeColorKey =
  | "background"
  | "foreground"
  | "card"
  | "cardForeground"
  | "popover"
  | "popoverForeground"
  | "primary"
  | "primaryForeground"
  | "secondary"
  | "secondaryForeground"
  | "muted"
  | "mutedForeground"
  | "accent"
  | "accentForeground"
  | "destructive"
  | "destructiveForeground"
  | "border"
  | "windowBorder"
  | "input"
  | "ring"
  | "serverAccent"
  | "chart1"
  | "chart2"
  | "chart3"
  | "chart4"
  | "chart5"
  | "sidebar"
  | "sidebarForeground"
  | "sidebarPrimary"
  | "sidebarPrimaryForeground"
  | "sidebarAccent"
  | "sidebarAccentForeground"
  | "sidebarBorder"
  | "sidebarRing"

export type AppThemeColors = Record<AppThemeColorKey, string>

export interface EditableAppTheme {
  key: string
  name: string
  mode: AppThemeMode
  colors: AppThemeColors
}

export interface AppThemeField {
  key: AppThemeColorKey
  cssVariable: string
  labelKey: string
}

export interface AppThemeGroup {
  key: string
  labelKey: string
  fields: AppThemeField[]
}

const field = (key: AppThemeColorKey, labelKey: string): AppThemeField => ({
  key,
  cssVariable: `--${key.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)}`,
  labelKey,
})

export const APP_THEME_FIELDS: AppThemeField[] = [
  field("background", "settings.themeColor.background"),
  field("foreground", "settings.themeColor.foreground"),
  field("card", "settings.themeColor.card"),
  field("cardForeground", "settings.themeColor.cardForeground"),
  field("popover", "settings.themeColor.popover"),
  field("popoverForeground", "settings.themeColor.popoverForeground"),
  field("primary", "settings.themeColor.primary"),
  field("primaryForeground", "settings.themeColor.primaryForeground"),
  field("secondary", "settings.themeColor.secondary"),
  field("secondaryForeground", "settings.themeColor.secondaryForeground"),
  field("muted", "settings.themeColor.muted"),
  field("mutedForeground", "settings.themeColor.mutedForeground"),
  field("accent", "settings.themeColor.accent"),
  field("accentForeground", "settings.themeColor.accentForeground"),
  field("destructive", "settings.themeColor.destructive"),
  field("destructiveForeground", "settings.themeColor.destructiveForeground"),
  field("border", "settings.themeColor.border"),
  field("windowBorder", "settings.themeColor.windowBorder"),
  field("input", "settings.themeColor.input"),
  field("ring", "settings.themeColor.ring"),
  {...field("serverAccent", "settings.themeColor.serverAccent"), cssVariable: "--server-accent-token"},
  field("chart1", "settings.themeColor.chart1"),
  field("chart2", "settings.themeColor.chart2"),
  field("chart3", "settings.themeColor.chart3"),
  field("chart4", "settings.themeColor.chart4"),
  field("chart5", "settings.themeColor.chart5"),
  field("sidebar", "settings.themeColor.sidebar"),
  field("sidebarForeground", "settings.themeColor.sidebarForeground"),
  field("sidebarPrimary", "settings.themeColor.sidebarPrimary"),
  field("sidebarPrimaryForeground", "settings.themeColor.sidebarPrimaryForeground"),
  field("sidebarAccent", "settings.themeColor.sidebarAccent"),
  field("sidebarAccentForeground", "settings.themeColor.sidebarAccentForeground"),
  field("sidebarBorder", "settings.themeColor.sidebarBorder"),
  field("sidebarRing", "settings.themeColor.sidebarRing"),
]

const fieldMap = new Map(APP_THEME_FIELDS.map((item) => [item.key, item]))

const colors = (values: Partial<AppThemeColors>): AppThemeColors => {
  const result = {} as AppThemeColors
  for (const item of APP_THEME_FIELDS) result[item.key] = values[item.key] ?? "#000000"
  return result
}

export const APP_THEME_GROUPS: AppThemeGroup[] = [
  {
    key: "surfaces",
    labelKey: "settings.themeSurfaces",
    fields: APP_THEME_FIELDS.filter(({key}) => ["background", "card", "popover", "secondary", "muted", "accent"].includes(key)),
  },
  {
    key: "content",
    labelKey: "settings.themeContent",
    fields: APP_THEME_FIELDS.filter(({key}) => ["foreground", "cardForeground", "popoverForeground", "primary", "primaryForeground", "secondaryForeground", "mutedForeground", "accentForeground"].includes(key)),
  },
  {
    key: "states",
    labelKey: "settings.themeStates",
    fields: APP_THEME_FIELDS.filter(({key}) => ["destructive", "destructiveForeground", "border", "windowBorder", "input", "ring", "serverAccent"].includes(key)),
  },
  {
    key: "charts",
    labelKey: "settings.themeCharts",
    fields: APP_THEME_FIELDS.filter(({key}) => key.startsWith("chart")),
  },
  {
    key: "sidebar",
    labelKey: "settings.themeSidebar",
    fields: APP_THEME_FIELDS.filter(({key}) => key.startsWith("sidebar")),
  },
]

const DEFAULT_LIGHT_COLORS = colors({
  background: "#ffffff",
  foreground: "#252525",
  card: "#ffffff",
  cardForeground: "#252525",
  popover: "#ffffff",
  popoverForeground: "#252525",
  primary: "#343434",
  primaryForeground: "#fafafa",
  secondary: "#f5f5f5",
  secondaryForeground: "#343434",
  muted: "#f5f5f5",
  mutedForeground: "#707070",
  accent: "#f5f5f5",
  accentForeground: "#343434",
  destructive: "#d63b32",
  destructiveForeground: "#fafafa",
  border: "#e8e8e8",
  windowBorder: "#d8d8d8",
  input: "#e8e8e8",
  ring: "#a3a3a3",
  serverAccent: "#159b7c",
  chart1: "#e6873a",
  chart2: "#3e9b9b",
  chart3: "#426f8d",
  chart4: "#c9a642",
  chart5: "#c78438",
  sidebar: "#fafafa",
  sidebarForeground: "#252525",
  sidebarPrimary: "#343434",
  sidebarPrimaryForeground: "#fafafa",
  sidebarAccent: "#f5f5f5",
  sidebarAccentForeground: "#343434",
  sidebarBorder: "#e8e8e8",
  sidebarRing: "#a3a3a3",
})

const DEFAULT_DARK_COLORS = colors({
  background: "#252525",
  foreground: "#fafafa",
  card: "#353535",
  cardForeground: "#fafafa",
  popover: "#353535",
  popoverForeground: "#fafafa",
  primary: "#ebebeb",
  primaryForeground: "#353535",
  secondary: "#454545",
  secondaryForeground: "#fafafa",
  muted: "#454545",
  mutedForeground: "#b5b5b5",
  accent: "#454545",
  accentForeground: "#fafafa",
  destructive: "#df7474",
  destructiveForeground: "#fafafa",
  border: "#555555",
  windowBorder: "#666666",
  input: "#606060",
  ring: "#909090",
  serverAccent: "#67d2a7",
  chart1: "#6589ef",
  chart2: "#56c5a1",
  chart3: "#e2b45b",
  chart4: "#bd7fe5",
  chart5: "#e57979",
  sidebar: "#353535",
  sidebarForeground: "#fafafa",
  sidebarPrimary: "#6589ef",
  sidebarPrimaryForeground: "#fafafa",
  sidebarAccent: "#454545",
  sidebarAccentForeground: "#fafafa",
  sidebarBorder: "#555555",
  sidebarRing: "#909090",
})

export const BUILT_IN_APP_THEMES: EditableAppTheme[] = [
  {key: "default-dark", name: "Default Dark", mode: "dark", colors: DEFAULT_DARK_COLORS},
  {key: "default-light", name: "Default Light", mode: "light", colors: DEFAULT_LIGHT_COLORS},
  {
    key: "midnight-blue",
    name: "Midnight Blue",
    mode: "dark",
    colors: colors({...DEFAULT_DARK_COLORS, background: "#101828", card: "#17243a", popover: "#17243a", primary: "#8ac8ff", primaryForeground: "#102038", secondary: "#203250", muted: "#203250", accent: "#294064", ring: "#6ba8df", serverAccent: "#71d4bb", windowBorder: "#35527b", sidebar: "#142238", sidebarPrimary: "#8ac8ff", sidebarAccent: "#203250", border: "#2e4669", input: "#38557b"}),
  },
  {
    key: "forest-terminal",
    name: "Forest Terminal",
    mode: "dark",
    colors: colors({...DEFAULT_DARK_COLORS, background: "#101a17", card: "#162721", popover: "#162721", primary: "#77e0b1", primaryForeground: "#102018", secondary: "#20372e", muted: "#20372e", accent: "#28513f", ring: "#63bc96", serverAccent: "#77e0b1", windowBorder: "#315443", sidebar: "#13231d", sidebarPrimary: "#77e0b1", sidebarAccent: "#20372e", border: "#315443", input: "#3a6652", chart1: "#77e0b1", chart2: "#67b5dc", chart3: "#e7bd68", chart4: "#c995ea", chart5: "#ed8888"}),
  },
  {
    key: "warm-paper",
    name: "Warm Paper",
    mode: "light",
    colors: colors({...DEFAULT_LIGHT_COLORS, background: "#f7f3eb", card: "#fffdf8", popover: "#fffdf8", foreground: "#342f2a", cardForeground: "#342f2a", primary: "#9a4d2e", primaryForeground: "#fff8ef", secondary: "#ece4d8", secondaryForeground: "#493d33", muted: "#ece4d8", mutedForeground: "#796d61", accent: "#e7d5c2", accentForeground: "#493d33", border: "#d8cbbb", windowBorder: "#c9b8a6", input: "#d8cbbb", ring: "#bd8c6a", serverAccent: "#438c73", sidebar: "#efe7dc", sidebarForeground: "#342f2a", sidebarPrimary: "#9a4d2e", sidebarPrimaryForeground: "#fff8ef", sidebarAccent: "#e7d5c2", sidebarAccentForeground: "#493d33", sidebarBorder: "#d8cbbb", sidebarRing: "#bd8c6a"}),
  },
]

export const DEFAULT_APP_THEME_KEY = "default-dark"

export function normalizeAppTheme(theme: AppTheme | EditableAppTheme): EditableAppTheme {
  const mode: AppThemeMode = theme.mode === "light" ? "light" : "dark"
  const base = BUILT_IN_APP_THEMES.find((item) => item.mode === mode) ?? BUILT_IN_APP_THEMES[0]
  const rawColors = theme.colors ?? {}
  const merged = {} as AppThemeColors
  for (const item of APP_THEME_FIELDS) {
    merged[item.key] = rawColors[item.key] ?? base.colors[item.key]
  }
  return {
    key: theme.key.trim() || base.key,
    name: theme.name.trim() || base.name,
    mode,
    colors: merged,
  }
}

export function mergeAppThemeStore(store?: AppThemeStore | null): {activeKey: string; themes: EditableAppTheme[]} {
  const builtInKeys = new Set(BUILT_IN_APP_THEMES.map((theme) => theme.key))
  const customThemes = (store?.themes ?? [])
    .map(normalizeAppTheme)
    .filter((theme) => !builtInKeys.has(theme.key))
  const themes = [...BUILT_IN_APP_THEMES, ...customThemes]
  const activeKey = themes.some((theme) => theme.key === store?.activeKey)
    ? store?.activeKey ?? DEFAULT_APP_THEME_KEY
    : DEFAULT_APP_THEME_KEY
  return {activeKey, themes}
}

export function applyAppTheme(theme: EditableAppTheme): void {
  if (typeof document === "undefined") return
  const root = document.documentElement
  root.classList.toggle("dark", theme.mode === "dark")
  root.style.colorScheme = theme.mode
  for (const item of APP_THEME_FIELDS) {
    root.style.setProperty(item.cssVariable, theme.colors[item.key])
  }
}

export function isValidCssColor(value: string): boolean {
  if (typeof CSS === "undefined" || typeof CSS.supports !== "function") return /^#[0-9a-f]{3,8}$/i.test(value.trim())
  return CSS.supports("color", value.trim())
}

export function toColorInputValue(value: string): string {
  const normalized = value.trim()
  return /^#[0-9a-f]{6}$/i.test(normalized) ? normalized : "#000000"
}

export function getAppThemeField(key: AppThemeColorKey): AppThemeField {
  return fieldMap.get(key) ?? APP_THEME_FIELDS[0]
}
