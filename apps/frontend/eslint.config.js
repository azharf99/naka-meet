import js from '@eslint/js';
import globals from 'globals';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': [
        'warn',
        { allowConstantExport: true },
      ],
      // The codebase intentionally leaves unused function params for
      // documented-but-unused callback signatures (see e.g. RoomManager
      // mock args in tests) — matches tsconfig.json's own
      // noUnusedLocals/noUnusedParameters: false. Still flag unused vars
      // since those are almost always real dead code, not intentional.
      '@typescript-eslint/no-unused-vars': [
        'warn',
        { args: 'none', varsIgnorePattern: '^_' },
      ],
      // eslint-plugin-react-hooks 7's "recommended" set added this rule for
      // the React Compiler and it's stricter than this codebase's
      // established patterns warrant: App.tsx's mount-only session-restore
      // and egress auto-join effects, and VideoGrid's stream-driven
      // hasVideo/hasAudio sync, are all deliberate (see their own comments)
      // rather than the setState-cascades-into-another-render anti-pattern
      // the rule targets. Kept visible as a warning rather than silenced —
      // worth revisiting case by case — but not a hard CI blocker.
      'react-hooks/set-state-in-effect': 'warn',
    },
  },
  {
    // Test files across this codebase mock browser/WebRTC globals
    // (RTCPeerConnection, WebSocket, MediaStream) per TESTING_STRATEGY.md's
    // own guidance to stub them — that inherently needs loose typing that
    // production code shouldn't reach for.
    files: ['**/*.test.ts', '**/*.test.tsx'],
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
    },
  },
);
