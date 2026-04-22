import { createContext, useContext, useState, useEffect, type ReactNode } from 'react'

type Theme = 'dark' | 'light'
type UIStyle = 'clean-saas' | 'cyber-terminal' | 'glassmorphism-lite'

interface AccentPreset {
  name: string
  color: string
  lightColor: string
}

const ACCENT_PRESETS: AccentPreset[] = [
  { name: 'Indigo', color: '#818cf8', lightColor: '#6366f1' },
  { name: 'Violet', color: '#a78bfa', lightColor: '#8b5cf6' },
  { name: 'Sky', color: '#7dd3fc', lightColor: '#0ea5e9' },
  { name: 'Green', color: '#86efac', lightColor: '#22c55e' },
  { name: 'Amber', color: '#fbbf24', lightColor: '#d97706' },
  { name: 'Pink', color: '#f472b6', lightColor: '#db2777' },
  { name: 'Orange', color: '#fb923c', lightColor: '#ea580c' },
]

interface ThemeContextType {
  theme: Theme
  setTheme: (theme: Theme) => void
  uiStyle: UIStyle
  setUIStyle: (style: UIStyle) => void
  accentColor: string
  setAccentColor: (color: string) => void
  accentPresets: AccentPreset[]
}

const ThemeContext = createContext<ThemeContextType>({
  theme: 'dark',
  setTheme: () => {},
  uiStyle: 'clean-saas',
  setUIStyle: () => {},
  accentColor: '#818cf8',
  setAccentColor: () => {},
  accentPresets: ACCENT_PRESETS,
})

function hexToHSL(hex: string): { h: number; s: number; l: number } {
  const r = parseInt(hex.slice(1, 3), 16) / 255
  const g = parseInt(hex.slice(3, 5), 16) / 255
  const b = parseInt(hex.slice(5, 7), 16) / 255
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  let h = 0
  let s = 0
  const l = (max + min) / 2
  if (max !== min) {
    const d = max - min
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
    switch (max) {
      case r: h = ((g - b) / d + (g < b ? 6 : 0)) / 6; break
      case g: h = ((b - r) / d + 2) / 6; break
      case b: h = ((r - g) / d + 4) / 6; break
    }
  }
  return { h: h * 360, s: s * 100, l: l * 100 }
}

function applyAccentColor(color: string, theme: Theme) {
  const hsl = hexToHSL(color)
  const root = document.documentElement

  const baseLightness = theme === 'dark' ? 70 : 50
  const hoverLightness = theme === 'dark' ? 75 : 55
  const lightAlpha = theme === 'dark' ? 0.2 : 0.15
  const lighterAlpha = theme === 'dark' ? 0.1 : 0.08

  root.style.setProperty('--accent', `hsl(${hsl.h}, ${hsl.s}%, ${baseLightness}%)`)
  root.style.setProperty('--accent-hover', `hsl(${hsl.h}, ${hsl.s}%, ${hoverLightness}%)`)
  root.style.setProperty('--accent-light', `hsla(${hsl.h}, ${hsl.s}%, ${baseLightness}%, ${lightAlpha})`)
  root.style.setProperty('--accent-lighter', `hsla(${hsl.h}, ${hsl.s}%, ${baseLightness}%, ${lighterAlpha})`)
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(() => {
    const stored = localStorage.getItem('theme')
    if (stored === 'light' || stored === 'dark') return stored
    return 'dark'
  })

  const [uiStyle, setUIStyleState] = useState<UIStyle>(() => {
    const stored = localStorage.getItem('uiStyle')
    if (stored === 'clean-saas' || stored === 'cyber-terminal' || stored === 'glassmorphism-lite') return stored
    return 'clean-saas'
  })

  const [accentColor, setAccentColorState] = useState<string>(() => {
    return localStorage.getItem('accentColor') || '#818cf8'
  })

  useEffect(() => {
    document.documentElement.classList.remove('light', 'dark')
    document.documentElement.classList.add(theme)
    document.documentElement.setAttribute('data-ui-style', uiStyle)
    localStorage.setItem('theme', theme)
    localStorage.setItem('uiStyle', uiStyle)
    applyAccentColor(accentColor, theme)
  }, [theme, uiStyle, accentColor])

  const setTheme = (t: Theme) => {
    setThemeState(t)
  }

  const setUIStyle = (s: UIStyle) => {
    setUIStyleState(s)
  }

  const setAccentColor = (color: string) => {
    setAccentColorState(color)
    localStorage.setItem('accentColor', color)
  }

  return (
    <ThemeContext.Provider value={{ theme, setTheme, uiStyle, setUIStyle, accentColor, setAccentColor, accentPresets: ACCENT_PRESETS }}>
      {children}
    </ThemeContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export function useTheme() {
  return useContext(ThemeContext)
}
