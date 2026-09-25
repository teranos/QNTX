import { test, expect } from 'bun:test';
import { fmt, renderAccount, renderSent, renderTemplates, reportSentLine, type MailRow, type MailTemplates } from './mail-element';

function mail(overrides: Partial<MailRow> = {}): MailRow {
    return {
        id: 'AS-USTIM-MAILSENT-NEUTRAL-66ZRW4UH',
        at: '2026-09-24T14:03:11.123Z',
        user: 'UStim',
        to: 'tim@defacile.nl',
        plugin: 'garden',
        template: 'neutral',
        subject: 'Welkom',
        sent: true,
        message_id: '0100019a-ses',
        error: '',
        ...overrides,
    };
}

// "its attested": the window reads each mail back from its attestation,
// sent and refused alike.
test('a sent mail carries its message id and a refused one what the transport said', () => {
    const container = document.createElement('div');
    renderSent(container, [
        mail({ sent: false, message_id: '', error: 'MessageRejected: Email address is not verified' }),
        mail(),
    ]);
    const rows = container.querySelectorAll('tbody tr');
    expect(rows.length).toBe(2);
    expect(rows[0].textContent).toContain('refused');
    expect(rows[0].textContent).toContain('MessageRejected');
    expect(rows[1].textContent).toContain('sent');
    expect(rows[1].textContent).toContain('0100019a-ses');
    expect(rows[1].textContent).toContain('tim@defacile.nl');
});

test('no mail is said, not left blank', () => {
    const container = document.createElement('div');
    renderSent(container, []);
    expect(container.textContent).toContain('No mail sent yet.');
    expect(container.querySelector('table')).toBeNull();
});

// "ses being enabled for use with email service can be enabled in the am.toml"
test('SES not enabled is said, and no quota is drawn', () => {
    const container = document.createElement('div');
    renderAccount(container, {
        from: 'Garden <mail@garden.test>',
        ses: { enabled: false, region: '' },
        account: null,
        unanswered: 'SES is not enabled: mail.ses.enabled is false in am.toml, so no mail is sent',
    });
    expect(container.textContent).toContain('not enabled');
    expect(container.textContent).toContain('mail.ses.enabled');
    expect(container.textContent).not.toContain('Sent in 24 hours');
});

test('what SES says of the account is drawn as it said it', () => {
    const container = document.createElement('div');
    renderAccount(container, {
        from: 'Garden <mail@garden.test>',
        ses: { enabled: true, region: 'eu-west-1' },
        account: {
            region: 'eu-west-1', production_access: true, sending_enabled: true,
            enforcement_status: 'HEALTHY', max_24_hour_send: 50000, max_send_rate: 14, sent_last_24_hours: 12,
        },
        unanswered: '',
    });
    expect(container.textContent).toContain('enabled in eu-west-1');
    expect(container.textContent).toContain('Production access:yes');
    expect(container.textContent).toContain('12 of 50000');
    expect(container.textContent).toContain('14 per second');
    expect(container.querySelector('.element-error')).toBeNull();
});

// "the plugin owns the template but qntx does provide a neutral template and code for how to set it"
test('the neutral template and each plugin template are shown as source, never rendered', () => {
    const templates: MailTemplates = {
        neutral: {
            id: '', at: '', plugin: 'qntx', version: '', name: 'neutral',
            subject: '{{.subject}}', html: '<p>{{.body}}</p>', text: '{{.body}}',
            values: ['subject', 'body'],
        },
        templates: [{
            id: 'AS-GARDEN-BOOKING', at: '2026-09-24T14:00:00Z', plugin: 'garden', version: '0.4.1',
            name: 'booking-accepted', subject: 'Boeking bevestigd voor {{.date}}',
            html: '<p>Tot {{.date}}.</p>', text: 'Tot {{.date}}.', values: [],
        }],
    };
    const container = document.createElement('div');
    renderTemplates(container, templates);
    expect(container.textContent).toContain('takes subject, body');
    expect(container.textContent).toContain('garden v0.4.1');
    expect(container.textContent).toContain('booking-accepted');
    expect(container.textContent).toContain('<p>Tot {{.date}}.</p>');
    expect(container.querySelector('p')).toBeNull();
});

// "for a user that is one of the root identities, i want to receive a weekly report."
test('the report control says where the report went and what SES called it', () => {
    expect(reportSentLine({ to: 'root@garden.test', message_id: '0100019a-ses', attestation_id: 'AS-X' }))
        .toBe('Sent to root@garden.test — 0100019a-ses');
});

test('an attestation time reads to the second', () => {
    expect(fmt('2026-09-24T14:03:11.123Z')).toBe('2026-09-24 14:03:11');
    expect(fmt('')).toBe('—');
});
