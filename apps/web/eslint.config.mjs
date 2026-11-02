import jsxA11y from "eslint-plugin-jsx-a11y";
import react from "eslint-plugin-react";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import ts from "typescript-eslint";
// The rules eslint-config-next applied before the move to React Router:
// TypeScript's recommended set, React's, the hooks rules (including the React
// Compiler checks) and its subset of accessibility checks.
const adminOnly =
  "Admin code is only for the admin build: import it from the admin pages and modules (pages/admin, components/app/admin, lib/admin*, lib/api/admin, types/admin), never from code the app loads.";
// An import of an admin module, however it's written: through @/ or a
// relative path, with "..", an extension or a /src/ path. Kept in sync with
// isAdminModule (web-build.ts), which the app build checks itself.
const adminImport =
  "(^|/)(admin|admin-routes|admin-layout|use-admin-resource)(\\.[cm]?[jt]sx?)?$|(^|/)admin/|(^|/)lib/admin";
const config = ts.config(
  {
    ignores: [
      "build/**",
      ".react-router/**",
      "playwright-report/**",
      "test-results/**",
    ],
  },
  ...ts.configs.recommended,
  {
    plugins: {
      react,
      "react-hooks": reactHooks,
      "jsx-a11y": jsxA11y,
    },
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    settings: { react: { version: "detect" } },
    rules: {
      ...react.configs.recommended.rules,
      ...reactHooks.configs.recommended.rules,
      "react/no-unknown-property": "off",
      "react/react-in-jsx-scope": "off",
      // Types describe props; React's runtime prop-types aren't used.
      "react/prop-types": "off",
      "react/jsx-no-target-blank": "off",
      // Every page escapes what it renders, the admin pages' other people's
      // names and emails included: no raw HTML anywhere.
      "react/no-danger": "error",
      "jsx-a11y/alt-text": ["warn", { elements: ["img"] }],
      "jsx-a11y/aria-props": "warn",
      "jsx-a11y/aria-proptypes": "warn",
      "jsx-a11y/aria-unsupported-elements": "warn",
      "jsx-a11y/role-has-required-aria-props": "warn",
      "jsx-a11y/role-supports-aria-props": "warn",
      "@typescript-eslint/no-unused-vars": "warn",
      "@typescript-eslint/no-unused-expressions": "warn",
    },
  },
  // Admin code stays in the admin build (plans/features/admin/README.md,
  // rule 7): only the admin pages and the admin modules themselves may
  // import it, so no module the app loads pulls it into the app build. The
  // app build also fails on any admin module (no-admin-modules in
  // vite.config.ts), and scripts/check-app-build.sh reads its output.
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: [
      "src/pages/admin/**",
      "src/components/app/admin/**",
      "src/components/layout/admin-layout.tsx",
      "src/lib/admin.tsx",
      "src/lib/admin-routes.ts",
      "src/lib/use-admin-resource.ts",
      "src/lib/api/admin.ts",
      "src/lib/api/admin.test.ts",
      "src/types/admin.ts",
    ],
    rules: {
      "no-restricted-imports": [
        "error",
        { patterns: [{ regex: adminImport, message: adminOnly }] },
      ],
      // import("…") of an admin module, which no-restricted-imports doesn't
      // see. The selector's regex can't hold a "/", so it's written \u002F.
      "no-restricted-syntax": [
        "error",
        {
          selector: `ImportExpression[source.value=/${adminImport.replaceAll("/", "\\u002F")}/]`,
          message: adminOnly,
        },
        {
          selector: "ImportExpression[source.type='TemplateLiteral']",
          message:
            "Write import() with a plain string, so the admin-code checks can read it.",
        },
      ],
    },
  },
);
export default config;
