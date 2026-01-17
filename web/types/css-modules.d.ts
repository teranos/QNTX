/**
 * CSS Modules type declarations
 *
 * Allows TypeScript to understand CSS module imports
 */

declare module '*.module.css' {
    const classes: { [key: string]: string };
    export default classes;
}
