// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import js from '@eslint/js';
import prettier from 'eslint-config-prettier';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import globals from 'globals';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist', '.dist-language', 'src/api/schema.d.ts', 'scripts/testdata'] },
  // No eslint-disable comments in the code (PRJ-005): an exception stands
  // here, per file and rule, where the class file covers it.
  { linterOptions: { noInlineConfig: true, reportUnusedDisableDirectives: 'error' } },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, ...tseslint.configs.strict, prettier],
    languageOptions: {
      ecmaVersion: 2023,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    },
  },
  {
    files: [
      'src/**/*.{js,jsx,mjs,cjs,ts,tsx,mts,cts}',
      'scripts/testdata/*/src/**/*.{js,jsx,mjs,cjs,ts,tsx,mts,cts}',
    ],
    rules: {
      // Fitness function render (PRJ-005): no HTML from strings; foreign
      // data goes through ForeignText.
      'no-restricted-syntax': [
        'error',
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: 'Use ForeignText.',
        },
        {
          selector: 'AssignmentExpression[left.property.name=/^(innerHTML|outerHTML)$/]',
          message: 'No HTML from strings.',
        },
        {
          selector: "CallExpression[callee.property.name='insertAdjacentHTML']",
          message: 'No HTML from strings.',
        },
        {
          selector:
            "CallExpression[callee.object.name='document'][callee.property.name=/^write(ln)?$/]",
          message: 'No HTML from strings.',
        },
      ],
    },
  },
  {
    // Playwright fixtures call use(), which is no React hook.
    files: ['e2e/**/*.ts', 'playwright.config.ts'],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
    rules: { 'react-hooks/rules-of-hooks': 'off' },
  },
  // The exceptions last, so no later block turns the rules on again.
  {
    // Control characters are what these checks look for.
    files: ['src/lib/commandform.ts', 'src/lib/logfilter.ts'],
    rules: { 'no-control-regex': 'off' },
  },
  {
    // TanStack Virtual keeps its own state; the React Compiler skips it.
    files: ['src/pages/server/ConsoleTab.tsx'],
    rules: { 'react-hooks/incompatible-library': 'off' },
  },
);
