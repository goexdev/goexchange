/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: '#0b0e11',
        panel: '#161a1f',
        border: '#262b30',
        brand: '#fcd535',
        success: '#02c076',
        danger: '#f6465d',
        muted: '#848e9c',
      },
    },
  },
  plugins: [],
}