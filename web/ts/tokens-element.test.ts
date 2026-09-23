import { describe, test, expect } from 'bun:test';
import { ago, ended, owner, renderList } from './tokens-element';

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
    renderList(container, [token(extra)], NOW);
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

// Revoking and enabling are the token element's, and a row is a way in to it.
test('a row offers no revoke or enable', () => {
    const container = document.createElement('div');
    renderList(container, [token(), token({ id: 'AT_2', revoked_at: before(DAY) })], NOW);
    expect(container.textContent).not.toContain('Revoke');
    expect(container.textContent).not.toContain('Enable');
});

describe('one column says whether a token is on and when it was last used', () => {
    describe('tim', () => {
        test('Last used is no longer a column of its own', () => {
            const container = document.createElement('div');
            renderList(container, [token()], NOW);
            const headers = [...container.querySelectorAll('th')].map(th => th.firstChild?.textContent?.trim());
            expect(headers).toEqual(['Label', 'Kind', 'Owner', 'DID', 'Namespace', 'Created', 'Status']);
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
                .querySelector<HTMLElement>('.element-pill-off');
            expect(pill?.textContent).toBe('revoked:4 days ago');
            expect(pill?.title).toBe('last used 5 days ago');
        });
    });

    describe('spike', () => {
        test('a revoked token nobody ever used hovers never used', () => {
            const pill = status({ revoked_at: before(4 * DAY) }).querySelector<HTMLElement>('.element-pill-off');
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

/** The cells of one row, by header. */
function row(extra: Record<string, string> = {}): Record<string, HTMLElement> {
    const container = document.createElement('div');
    renderList(container, [token(extra)], NOW);
    const names = [...container.querySelectorAll('th')].map(th => th.firstChild?.textContent?.trim() ?? '');
    const cells = [...container.querySelectorAll<HTMLElement>('tbody td')];
    return Object.fromEntries(names.map((name, i) => [name, cells[i]]));
}

describe('what a row says', () => {
    describe('tim', () => {
        // "it should just say Owner, tokens are Owned by someone."
        test('the owner is the name of whoever minted it, and the identity is on the hover', () => {
            const cells = row({ minted_by_display_name: 'root' });
            expect(cells.Owner.textContent).toBe('root');
            expect(cells.Owner.title).toBe('apple:001750');
        });

        test('the kind is a column of its own', () => {
            expect(row({ level: 'REFRESH' }).Kind.textContent).toBe('REFRESH');
        });

        // "Created date should not also have to include time, unless hover"
        test('created is the day, and the time is on the hover', () => {
            const cells = row();
            expect(cells.Created.textContent).toBe('2026-09-14');
            expect(cells.Created.title).toBe('2026-09-14 00:43:56');
        });

        // "same for expired date, does not need to show immediately, unless hover"
        test('an expired token reads expired, and when is on the hover', () => {
            const pill = status({ expires_at: before(2 * HOUR) }).querySelector<HTMLElement>('.element-pill-past');
            expect(pill?.textContent).toBe('expired');
            expect(pill?.parentElement?.textContent).toBe('expired');
            expect(pill?.title).toBe(`expired ${new Date(NOW.getTime() - 2 * HOUR).toISOString().slice(0, 19).replace('T', ' ')}`);
        });

        // A refresh token is spent at the token endpoint and is never a bearer,
        // so "never used" said nothing about it.
        test('a live refresh token reads active and nothing about use', () => {
            const cell = status({ level: 'REFRESH' });
            expect(cell.querySelector('.token-pill-active')?.textContent).toBe('active');
            expect(cell.querySelector('.token-pill-never')).toBeNull();
        });
    });

    describe('spike', () => {
        test('the owner falls back to the identity when no name was recorded', () => {
            expect(owner({ minted_by: 'apple:001750' })).toBe('apple:001750');
            expect(owner({})).toBe('—');
        });

        test('a token has ended when it is revoked or past its expiry, and not before', () => {
            expect(ended({}, NOW)).toBe(false);
            expect(ended({ expires_at: new Date(NOW.getTime() + HOUR).toISOString() }, NOW)).toBe(false);
            expect(ended({ expires_at: before(HOUR) }, NOW)).toBe(true);
            expect(ended({ revoked_at: before(HOUR) }, NOW)).toBe(true);
        });
    });
});

// "I wish the status col would give me a way to filter out everything revoked and expired."
test('the switch in the Status header hides what no longer works, and shows it again', () => {
    const container = document.createElement('div');
    renderList(container, [
        token({ id: 'AT_1', label: 'live' }),
        token({ id: 'AT_2', label: 'revoked', revoked_at: before(DAY) }),
        token({ id: 'AT_3', label: 'expired', expires_at: before(HOUR) }),
    ], NOW);
    const labels = () => [...container.querySelectorAll('tbody tr td:first-child')].map(td => td.textContent);
    expect(labels()).toEqual(['live', 'revoked', 'expired']);

    container.querySelector<HTMLButtonElement>('.tokens-live-only')!.click();
    expect(labels()).toEqual(['live']);

    container.querySelector<HTMLButtonElement>('.tokens-live-only')!.click();
    expect(labels()).toEqual(['live', 'revoked', 'expired']);
});
