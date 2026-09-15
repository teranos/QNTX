import { describe, test, expect } from 'bun:test';
import { ago, renderList } from './tokens-glyph';

const NOW = new Date('2026-09-15T12:00:00Z');

function token(extra: Record<string, string> = {}) {
    return {
        id: 'AT_1',
        label: 'pond-sensor',
        did: 'did:key:zPond',
        minted_by: 'apple:001750',
        namespaces: ['clean'],
        created_at: '2026-09-14T00:43:56Z',
        ...extra,
    };
}

/** The status cell of one row, as a token sees it: no switches, so it is the last cell. */
function status(extra: Record<string, string> = {}): HTMLElement {
    const container = document.createElement('div');
    renderList(container, [token(extra)], false, NOW);
    const cell = container.querySelector<HTMLElement>('tbody td:last-child');
    if (!cell) throw new Error('no status cell rendered');
    return cell;
}

function before(ms: number): string {
    return new Date(NOW.getTime() - ms).toISOString();
}

const MINUTE = 60 * 1000;
const HOUR = 60 * 60 * 1000;
const DAY = 24 * 60 * 60 * 1000;

// A token lists and reads and revokes nothing, so a row does not offer it
// Revoke or Enable.
test('a token is not offered revoke or enable', () => {
    const container = document.createElement('div');
    renderList(container, [token()], false, NOW);
    expect(container.querySelector('button')).toBeNull();
});

describe('one column says whether a token is on and when it was last used', () => {
    describe('tim', () => {
        test('Last used is no longer a column of its own', () => {
            const container = document.createElement('div');
            renderList(container, [token()], false, NOW);
            const headers = [...container.querySelectorAll('th')].map(th => th.textContent);
            expect(headers).toEqual(['Label', 'For', 'DID', 'Namespace', 'Created', 'Status']);
        });

        test('an active token nobody has used reads active, never used', () => {
            const cell = status();
            expect(cell.querySelector('.token-pill-active')?.textContent).toBe('active');
            expect(cell.querySelector('.token-pill-never')?.textContent).toBe('never used');
        });

        test('an active token used within the minute reads active, now, both green', () => {
            const cell = status({ last_used_at: before(20 * 1000) });
            expect(cell.querySelector('.token-pill-active')?.textContent).toBe('active');
            expect(cell.querySelector('.token-age-now')?.textContent).toBe('now');
        });

        test('an active token used hours ago reads its age, coloured for hours', () => {
            const cell = status({ last_used_at: before(3 * HOUR) });
            expect(cell.querySelector('.token-age-hours')?.textContent).toBe('3 hours ago');
        });

        test('a revoked token reads when it was revoked, and hovers when it was last used', () => {
            const pill = status({ revoked_at: before(4 * DAY), last_used_at: before(5 * DAY) })
                .querySelector<HTMLElement>('.glyph-pill-off');
            expect(pill?.textContent).toBe('revoked:4 days ago');
            expect(pill?.title).toBe('last used 5 days ago');
        });
    });

    describe('spike', () => {
        test('a revoked token nobody ever used hovers never used', () => {
            const pill = status({ revoked_at: before(4 * DAY) }).querySelector<HTMLElement>('.glyph-pill-off');
            expect(pill?.title).toBe('never used');
        });

        test('a moment the clock has not reached yet reads now', () => {
            expect(ago(new Date(NOW.getTime() + 5 * MINUTE).toISOString(), NOW)).toEqual({ text: 'now', age: 'now' });
        });

        test('every unit is rounded down, and one reads singular', () => {
            const cases: Array<[number, string, string]> = [
                [59 * 1000, 'now', 'now'],
                [MINUTE, '1 minute ago', 'minutes'],
                [59 * MINUTE, '59 minutes ago', 'minutes'],
                [HOUR, '1 hour ago', 'hours'],
                [23 * HOUR, '23 hours ago', 'hours'],
                [DAY, '1 day ago', 'days'],
                [6 * DAY, '6 days ago', 'days'],
                [7 * DAY, '1 week ago', 'weeks'],
                [29 * DAY, '4 weeks ago', 'weeks'],
                [30 * DAY, '1 month ago', 'months'],
                [400 * DAY, '13 months ago', 'months'],
            ];
            for (const [elapsed, text, age] of cases) {
                expect(ago(before(elapsed), NOW)).toEqual({ text, age: age as ReturnType<typeof ago>['age'] });
            }
        });
    });
});
