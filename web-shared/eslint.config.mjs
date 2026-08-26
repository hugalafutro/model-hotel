// web-shared has no toolchain of its own: both apps compile it from source, and
// Biome already formats it from the dashboard's `format` script. ESLint covers
// it the same way, by reusing the dashboard's flat config verbatim rather than
// keeping a second rule set that could drift from it. `pnpm lint` in web/ runs
// it; the plugin imports resolve out of web/node_modules, where this config's
// real definition lives.
export { default } from "../web/eslint.config.js";
