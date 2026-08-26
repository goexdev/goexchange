import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'

export const LANGUAGES = [
  { code: 'en', name: 'English', flag: '🇺🇸', rtl: false },
  { code: 'zh', name: '中文', flag: '🇨🇳', rtl: false },
  { code: 'ja', name: '日本語', flag: '🇯🇵', rtl: false },
  { code: 'ko', name: '한국어', flag: '🇰🇷', rtl: false },
  { code: 'hi', name: 'हिन्दी', flag: '🇮🇳', rtl: false },
  { code: 'pt-BR', name: 'Português (BR)', flag: '🇧🇷', rtl: false },
  { code: 'fa', name: 'فارسی', flag: '🇮🇷', rtl: true },
  { code: 'ru', name: 'Русский', flag: '🇷🇺', rtl: false },
] as const

export type LanguageCode = typeof LANGUAGES[number]['code']

import enResources from './locales/en.json'

// Dynamic import map for lazy loading (other languages loaded on demand)
// Each language is a separate chunk that loads on demand
const loaders: Record<string, () => Promise<any>> = {
  en: () => import('./locales/en.json'),
  zh: () => import('./locales/zh.json'),
  ja: () => import('./locales/ja.json'),
  ko: () => import('./locales/ko.json'),
  hi: () => import('./locales/hi.json'),
  'pt-BR': () => import('./locales/pt-BR.json'),
  fa: () => import('./locales/fa.json'),
  ru: () => import('./locales/ru.json'),
}

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    // English is eagerly bundled inline (small, ~13KB) so first render has translations
    resources: {
      en: { translation: enResources },
    },
    fallbackLng: 'en',
    supportedLngs: ['en', 'zh', 'ja', 'ko', 'hi', 'pt-BR', 'fa', 'ru'],
    ns: ['translation'],
    defaultNS: 'translation',
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      lookupLocalStorage: 'goexchange_language',
    },
    interpolation: { escapeValue: false },
    react: {
      useSuspense: false,  // Don't suspend UI on language load
    },
  })

// Preload helper - loads language file and registers with i18next
async function loadLanguage(code: string): Promise<void> {
  const loader = loaders[code]
  if (!loader) return
  try {
    const module = await loader()
    i18n.addResourceBundle(code, 'translation', module.default, true, true)
  } catch (err) {
    console.error(`Failed to load language ${code}:`, err)
  }
}

// Auto-detect and load initial language
const allSupported = ['en', 'zh', 'ja', 'ko', 'hi', 'pt-BR', 'fa', 'ru']
const detectedLang = i18n.language || 'en'
// Try exact match first, then base language match
const initialLang = allSupported.includes(detectedLang)
  ? detectedLang
  : (allSupported.find(l => l.startsWith(detectedLang.split('-')[0])) || 'en')

// Load the initial language and switch to it
;(async () => {
  await loadLanguage(initialLang)
  if (i18n.language !== initialLang) {
    await i18n.changeLanguage(initialLang)
  }
})()

// Listen for language changes
i18n.on('languageChanged', async (lng) => {
  await loadLanguage(lng)
})

export { loadLanguage }
export default i18n
