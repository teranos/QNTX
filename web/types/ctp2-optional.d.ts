/**
 * Ambient module declarations for optional CTP2 modules.
 * These modules are private/local-only and may not exist in all environments.
 * TypeScript will allow imports but won't enforce their presence at compile time.
 *
 * Using wildcard pattern to match all ctp2 imports without type-checking actual files.
 */

declare module '../ctp2/*.js' {
  const content: any;
  export = content;
}

declare module '../ctp2/*' {
  const content: any;
  export = content;
}
