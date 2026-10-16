import { test, expect } from 'bun:test';
import { ESLint } from 'eslint';

const RECORD_IS_TABLE_CELL = 'a record, a label beside its value, is record() from ts/table-cell.ts: its labels never break inside a word.';

// "i wish very deeply that this was made structurally impossible, it should simply not be a thing, ci should fail on it"
test('a record table built outside record() stops the build', async () => {
    const eslint = new ESLint({ cwd: `${import.meta.dir}/..` });
    const code = "import { createTable } from '@teranos/elements';\nexport const t = createTable('x', ['Name', 'n'], []);\n";
    const [result] = await eslint.lintText(code, { filePath: 'ts/user-element.ts' });
    expect(result.messages.map(m => m.message)).toContain(RECORD_IS_TABLE_CELL);
});
