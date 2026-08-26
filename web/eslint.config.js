// ESLint flat config (ESLint v9+)
// Prevents i18n regression: hardcoded English text in JSX triggers errors.

import js from '@eslint/js';
import ts from '@typescript-eslint/parser';
import tsPlugin from '@typescript-eslint/eslint-plugin';
import reactPlugin from 'eslint-plugin-react';
import reactHooksPlugin from 'eslint-plugin-react-hooks';
import noHardcodedJsx from './eslint-plugins/no-hardcoded-jsx-text.js';

export default [
  // Base JS rules
  js.configs.recommended,

  // TypeScript + React files
  {
    files: ['src/**/*.{ts,tsx,js,jsx}'],
    languageOptions: {
      parser: ts,
      parserOptions: {
        ecmaVersion: 2022,
        sourceType: 'module',
        ecmaFeatures: { jsx: true },
      },
      globals: {
        window: 'readonly',
        document: 'readonly',
        console: 'readonly',
        setTimeout: 'readonly',
        clearTimeout: 'readonly',
        setInterval: 'readonly',
        clearInterval: 'readonly',
        localStorage: 'readonly',
        sessionStorage: 'readonly',
        fetch: 'readonly',
        URL: 'readonly',
        URLSearchParams: 'readonly',
        FormData: 'readonly',
        Headers: 'readonly',
        Response: 'readonly',
        Request: 'readonly',
        WebSocket: 'readonly',
        crypto: 'readonly',
        process: 'readonly',
        navigator: 'readonly',
        location: 'readonly',
        history: 'readonly',
        HTMLInputElement: 'readonly',
        HTMLButtonElement: 'readonly',
        HTMLElement: 'readonly',
        confirm: 'readonly',
        alert: 'readonly',
        prompt: 'readonly',
      },
    },
    plugins: {
      '@typescript-eslint': tsPlugin,
      'react': reactPlugin,
      'react-hooks': reactHooksPlugin,
      'no-hardcoded': noHardcodedJsx,
    },
    settings: {
      react: { version: '18.3' },
    },
    rules: {
      // THE CRITICAL RULE - prevents hardcoded English text in JSX
      'no-hardcoded/no-hardcoded-jsx-text': 'error',

      // Relaxed defaults (we're not trying to be perfect)
      'no-unused-vars': 'off', // TS handles this
      'no-undef': 'off',       // TS handles this
      'no-empty': 'warn',
      'no-extra-semi': 'error',
      'no-unreachable': 'warn',
      'no-unexpected-multiline': 'off',
      'no-constant-condition': 'warn',

      // TypeScript - minimal checks (TS already catches most)
      'no-unused-vars': 'off',
      'no-redeclare': 'off',
    },
  },

  // Config files
  {
    files: ['*.config.{js,ts,mjs}', 'eslint.config.js'],
    languageOptions: { globals: { process: 'readonly' } },
    rules: { 'no-hardcoded/no-hardcoded-jsx-text': 'off' },
  },

  // Test files (relaxed)
  {
    files: ['**/*.test.{ts,tsx}', 'tests/**/*'],
    rules: { 'no-hardcoded/no-hardcoded-jsx-text': 'off' },
  },

  // Ignore build artifacts
  {
    ignores: [
      'dist/**',
      'node_modules/**',
      'eslint-plugins/**', // Don't lint the plugin itself
    ],
  },
];
