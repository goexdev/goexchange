# ESLint Plugin: no-hardcoded-jsx-text

## Purpose

Prevents **i18n regression** by detecting hardcoded English text in JSX
content. All user-facing text should use `t()` from `react-i18next`.

## Rule

**`no-hardcoded-jsx-text`** - error

Flags any text in JSX that:
- Is longer than 1 character
- Starts with a capital letter (likely English text)
- Is not a known technical identifier (USDT, BTC, API, etc.)
- Is not pure emoji
- Is not pure punctuation

## Examples

### ❌ Wrong - will trigger error

```tsx
function MyComponent() {
  return (
    <div>
      <h1>Welcome to our app</h1>
      <p>This is hardcoded English</p>
    </div>
  );
}
```

```
error  Hardcoded text "Welcome to our app" in JSX. Use t() from react-i18next instead.
error  Hardcoded text "This is hardcoded English" in JSX. Use t() from react-i18next instead.
```

### ✅ Correct - no errors

```tsx
import { useTranslation } from 'react-i18next'

function MyComponent() {
  const { t } = useTranslation()
  return (
    <div>
      <h1>{t('home.welcome')}</h1>
      <p>{t('home.description')}</p>
      <span>USDT</span>  {/* Currency code - allowed */}
      <span>...</span>   {/* Punctuation - allowed */}
      <span>📊</span>   {/* Emoji - allowed */}
    </div>
  )
}
```

## What's Allowed

The rule has a smart allow-list to reduce false positives:

| Allowed | Examples |
|---|---|
| Technical identifiers | `USDT`, `BTC`, `API`, `UUID`, `2FA`, `KYC` |
| Currency tickers | `USDT`, `BTC`, `ETH`, `BNB`, `SOL` |
| Pure emoji | `📧`, `✓`, `⚠️` |
| Pure punctuation | `...`, `+`, `-`, `x` |
| CSS-like identifiers | `text-red-500` |
| PascalCase identifiers | `MyComponent` |
| Pure numbers | `123`, `0.01` |
| URLs | `https://...`, `mailto:...` |
| Locale codes | `en-US`, `pt-BR` |
| Trading pairs | `BTC_USDT`, `BNB_USDT` |
| Component names | `useState`, `React`, `Link` |

## Running the Linter

```bash
# Check all source files
npm run lint

# Auto-fix where possible (limited - t() replacement needs manual work)
npm run lint:fix
```

## Configuration

The rule is configured in `eslint.config.js`:

```js
import noHardcodedJsx from './eslint-plugins/no-hardcoded-jsx-text.js'

export default [
  {
    files: ['src/**/*.{ts,tsx,js,jsx}'],
    plugins: { 'no-hardcoded': noHardcodedJsx },
    rules: {
      'no-hardcoded/no-hardcoded-jsx-text': 'error',
    },
  },
  // Test files, configs, and build dirs are excluded
]
```

## Future Improvements

- [ ] Auto-fix to add `t()` call for known keys
- [ ] Integration with `en.json` to suggest missing keys
- [ ] Severity levels (warn vs error) per directory
- [ ] IDE integration (vscode-eslint)
