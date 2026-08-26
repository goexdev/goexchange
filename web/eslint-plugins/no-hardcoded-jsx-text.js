/**
 * ESLint Plugin: no-hardcoded-jsx-text
 *
 * Detects hardcoded English strings in JSX text content to prevent i18n regression.
 */

const ALLOWED_PATTERNS = [
  /^[a-z][a-z0-9-]*$/i,
  /^[A-Z][a-zA-Z0-9]*$/,
  /^\d+$/,
  /^(true|false|null|undefined)$/i,
  /^\//,
  /^[A-Z]{3,5}$/,
  /^[a-z]{2}-[A-Z]{2}$/,
  /^[{][\w.\s,]+[}]$/,
  /^[\$\{]/,
];

const ALLOWED_WORDS = new Set([
  "", " ", "/", "-", "+", "*", "#", "@", ":", ";", "!", "?", ".", ",",
  "...", "x", "*",
  "OK", "vs", "min", "max", "API", "UUID", "URL", "CSS", "HTML", "JSON",
  "WS", "L2", "2FA", "KYC", "TOTP", "HMAC", "JWT", "UTC", "TBD", "TODO",
  "BIP", "ERC", "EIP", "RPC", "HD", "UI", "UX",
  "USDT", "BTC", "ETH", "FBK", "LTC", "BNB", "SOL",
  "+", "-", "x", "/", "...",
]);

const IGNORE_KEYWORDS = new Set([
  "BTC_USDT", "BNB_USDT", "ETH_USDT", "LTC_USDT", "FBK_USDT",
  "BTCUSDT", "ETHUSDT", "LTCUSDT",
  "true", "false", "null", "undefined",
  "useState", "useEffect", "useCallback", "useMemo", "useRef",
  "useTranslation", "useNavigate", "useLocation", "useParams",
  "React", "Component", "Fragment",
  "NavLink", "Link", "Navigate",
  "div", "span", "h1", "h2", "h3", "h4", "h5", "h6",
  "p", "a", "button", "input", "form", "select", "option",
  "table", "thead", "tbody", "tr", "th", "td",
  "ul", "ol", "li", "img", "svg", "path", "circle", "g", "rect",
  "line", "text", "tspan",
]);

function isAllowedText(text) {
  const trimmed = text.trim();
  if (!trimmed) return true;
  if (trimmed.length < 2) return true;
  if (ALLOWED_WORDS.has(trimmed)) return true;
  if (IGNORE_KEYWORDS.has(trimmed)) return true;
  if (ALLOWED_PATTERNS.some((p) => p.test(trimmed))) return true;
  // Emoji-only strings are decorative
  if (/^[\p{Emoji}\s]+$/u.test(trimmed)) return true;
  return false;
}

const rule = {
  meta: {
    type: "problem",
    docs: {
      description: "Disallow hardcoded English text in JSX - use i18n t() instead",
      category: "i18n",
    },
    schema: [],
    messages: {
      hardcodedText:
        'Hardcoded text "{{text}}" in JSX. Use t() from react-i18next instead.',
    },
  },
  create(context) {
    return {
      JSXText(node) {
        const text = node.value;
        if (isAllowedText(text)) return;
        context.report({
          node,
          messageId: "hardcodedText",
          data: { text: text.trim().substring(0, 50) },
        });
      },
    };
  },
};

const plugin = {
  meta: { name: "no-hardcoded-jsx-text", version: "1.0.0" },
  rules: { "no-hardcoded-jsx-text": rule },
};

export default plugin;
