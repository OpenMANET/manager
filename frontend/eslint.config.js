import js from '@eslint/js';
import globals from 'globals';
import reactPlugin from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';

const vitestGlobals = {
  describe: 'readonly',
  it: 'readonly',
  test: 'readonly',
  expect: 'readonly',
  vi: 'readonly',
  beforeEach: 'readonly',
  afterEach: 'readonly',
  beforeAll: 'readonly',
  afterAll: 'readonly',
};

const reactRules = {
  ...js.configs.recommended.rules,
  ...reactPlugin.configs.recommended.rules,
  ...reactHooks.configs.recommended.rules,
  // Automatic JSX transform via @vitejs/plugin-react — no need to import React
  'react/react-in-jsx-scope': 'off',
  // prop-types is not installed in this project
  'react/prop-types': 'off',
};

export default [
  // Global ignores: auto-generated protobuf clients, node_modules, coverage reports
  { ignores: ['src/gen/**', 'node_modules/**', 'coverage/**'] },

  // Config files run in Node (vite.config.js, vitest.config.js, eslint.config.js)
  {
    files: ['*.config.js'],
    languageOptions: {
      globals: { ...globals.node },
      ecmaVersion: 'latest',
      sourceType: 'module',
    },
    rules: { ...js.configs.recommended.rules },
  },

  // Whisper WASM service — Module global is injected by index.html via Emscripten script tag
  {
    files: ['src/services/whisperService.js'],
    languageOptions: {
      globals: { ...globals.browser, Module: 'readonly' },
      ecmaVersion: 'latest',
      sourceType: 'module',
    },
    rules: { ...js.configs.recommended.rules },
  },

  // Application source files
  {
    files: ['src/**/*.{js,jsx}'],
    plugins: {
      react: reactPlugin,
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    languageOptions: {
      globals: { ...globals.browser },
      ecmaVersion: 'latest',
      sourceType: 'module',
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    settings: { react: { version: 'detect' } },
    rules: {
      ...reactRules,
      // allowConstantExport required for Vite HMR compatibility
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    },
  },

  // Test files — add vitest globals
  {
    files: ['src/__tests__/**/*.{js,jsx}'],
    plugins: {
      react: reactPlugin,
      'react-hooks': reactHooks,
    },
    languageOptions: {
      globals: { ...globals.browser, ...vitestGlobals },
      ecmaVersion: 'latest',
      sourceType: 'module',
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    settings: { react: { version: 'detect' } },
    rules: reactRules,
  },
];
