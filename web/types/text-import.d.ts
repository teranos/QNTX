// A stylesheet imported `with { type: 'text' }` is its text, bundled whole (the Design Element reads tokens.css so).
declare module '*.css' {
    const text: string;
    export default text;
}
