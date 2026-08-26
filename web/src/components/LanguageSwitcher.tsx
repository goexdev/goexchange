import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { LANGUAGES, LanguageCode, loadLanguage } from '../i18n'

export function LanguageSwitcher() {
  const { i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  // Set RTL direction on body for Persian
  useEffect(() => {
    const isRtl = LANGUAGES.find(l => l.code === i18n.language)?.rtl || false
    document.documentElement.dir = isRtl ? 'rtl' : 'ltr'
  }, [i18n.language])

  const current = LANGUAGES.find(l => l.code === i18n.language) || LANGUAGES[0]

  async function changeLang(code: LanguageCode) {
    setOpen(false)
    // Load language resources BEFORE switching (avoid showing literal keys)
    await loadLanguage(code)
    await i18n.changeLanguage(code)
  }

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setOpen(o => !o)}
        className="flex items-center gap-1 px-2 py-1 rounded bg-gray-700 hover:bg-gray-600 text-sm"
        aria-label="Select language"
      >
        <span>{current.flag}</span>
        <span className="hidden md:inline">{current.code.toUpperCase()}</span>
        <span className="text-xs">▼</span>
      </button>

      {open && (
        <div className="absolute right-0 mt-1 w-48 bg-panel border border-border rounded-lg shadow-xl z-50 max-h-80 overflow-y-auto">
          {LANGUAGES.map(lang => (
            <button
              key={lang.code}
              onClick={() => changeLang(lang.code)}
              className={`w-full text-left px-3 py-2 text-sm flex items-center gap-2 hover:bg-gray-700 ${
                lang.code === i18n.language ? 'bg-blue-900 text-blue-200' : 'text-gray-300'
              }`}
            >
              <span>{lang.flag}</span>
              <span>{lang.name}</span>
              {lang.rtl && <span className="text-xs text-yellow-400">RTL</span>}
              {lang.code === i18n.language && <span className="ml-auto text-xs">✓</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
