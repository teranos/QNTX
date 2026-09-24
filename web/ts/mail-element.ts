/**
 * Mail Element — what the node mails on plugins' behalf (ADR-041).
 */

// "that is a ROOT question about governance and controls and monitoring that belongs in its own window element in the tray like others"

// Plain window, reached from ⍟ by ROOT. One section per sigil of the mail
// signum: the account SES sends through, the templates mail is filled from,
// and every mail sent or refused. Nothing here sends.

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { log, SEG } from './logger';

/** One mail the node sent or tried to, as /api/mail gives it. */
export interface MailRow {
    id: string;
    at: string;
    user: string;
    to: string;
    plugin: string;
    template: string;
    subject: string;
    sent: boolean;
    message_id: string;
    error: string;
}

/** One template mail is filled from, as /api/mail/templates gives it. */
export interface MailTemplateRow {
    id: string;
    at: string;
    plugin: string;
    version: string;
    name: string;
    subject: string;
    html: string;
    text: string;
    values: string[];
}

export interface MailTemplates {
    neutral: MailTemplateRow;
    templates: MailTemplateRow[];
}

/** What SES says of the account, as /api/mail/account gives it. */
export interface SESAccount {
    region: string;
    production_access: boolean;
    sending_enabled: boolean;
    enforcement_status: string;
    max_24_hour_send: number;
    max_send_rate: number;
    sent_last_24_hours: number;
}

export interface MailAccount {
    from: string;
    ses: { enabled: boolean; region: string };
    account: SESAccount | null;
    unanswered: string;
}

const ELEMENT_ID = 'mail-element';

/** An attestation's time, to the second, in UTC. */
export function fmt(at: string): string {
    if (!at) return '—';
    return at.slice(0, 19).replace('T', ' ');
}

function row(label: string, value: string): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-row';
    const l = document.createElement('span');
    l.className = 'label';
    l.textContent = label;
    const v = document.createElement('span');
    v.className = 'element-value';
    v.textContent = value;
    div.appendChild(l);
    div.appendChild(v);
    return div;
}

function section(title: string): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-section';
    const h = document.createElement('h3');
    h.className = 'element-section-title';
    h.textContent = title;
    div.appendChild(h);
    return div;
}

function cell(text: string, className = ''): HTMLTableCellElement {
    const td = document.createElement('td');
    td.className = className;
    td.textContent = text;
    return td;
}

function said(text: string): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-loading';
    div.textContent = text;
    return div;
}

const yes = (b: boolean) => (b ? 'yes' : 'no');

/** Exported for tests: what the node sends through, and what SES says of it. */
export function renderAccount(container: HTMLElement, a: MailAccount): void {
    container.innerHTML = '';
    const s = section('Account');
    s.appendChild(row('From:', a.from || '— (mail.from is not set, so nothing is sent)'));
    s.appendChild(row('SES:', a.ses.enabled ? `enabled${a.ses.region ? ` in ${a.ses.region}` : ''}` : 'not enabled'));
    if (a.account) {
        s.appendChild(row('Region:', a.account.region));
        s.appendChild(row('Production access:', yes(a.account.production_access)));
        s.appendChild(row('Sending enabled:', yes(a.account.sending_enabled)));
        s.appendChild(row('Enforcement:', a.account.enforcement_status || '—'));
        s.appendChild(row('Sent in 24 hours:', `${a.account.sent_last_24_hours} of ${a.account.max_24_hour_send}`));
        s.appendChild(row('Send rate:', `${a.account.max_send_rate} per second`));
    }
    if (a.unanswered) {
        const why = document.createElement('div');
        why.className = 'element-error';
        why.textContent = a.unanswered;
        s.appendChild(why);
    }
    container.appendChild(s);
}

/** One template's parts, open on press. The source is shown as text: a
 *  template is read here, never rendered. */
function templateParts(t: MailTemplateRow): HTMLDetailsElement {
    const details = document.createElement('details');
    const summary = document.createElement('summary');
    summary.textContent = t.subject;
    details.appendChild(summary);
    for (const [label, body] of [['html', t.html], ['text', t.text]] as const) {
        if (!body) continue;
        const h = document.createElement('div');
        h.className = 'label';
        h.textContent = label;
        const pre = document.createElement('pre');
        pre.textContent = body;
        pre.style.whiteSpace = 'pre-wrap';
        pre.style.overflowWrap = 'break-word';
        pre.style.margin = '4px 0 8px';
        details.appendChild(h);
        details.appendChild(pre);
    }
    return details;
}

/** Exported for tests: QNTX's neutral template, and the newest each plugin set. */
export function renderTemplates(container: HTMLElement, t: MailTemplates): void {
    container.innerHTML = '';
    const s = section('Templates');

    const neutral = document.createElement('div');
    neutral.className = 'mail-neutral';
    neutral.appendChild(row('Neutral:', `filled when a plugin names none — takes ${t.neutral.values.join(', ')}`));
    neutral.appendChild(templateParts(t.neutral));
    s.appendChild(neutral);

    if (t.templates.length === 0) {
        s.appendChild(said('No plugin has set a template.'));
        container.appendChild(s);
        return;
    }

    const table = document.createElement('table');
    table.className = 'element-table mail-templates-table';
    const thead = document.createElement('thead');
    thead.innerHTML = '<tr><th>Plugin</th><th>Name</th><th>Set</th><th>Subject</th></tr>';
    table.appendChild(thead);
    const tbody = document.createElement('tbody');
    for (const kept of t.templates) {
        const tr = document.createElement('tr');
        const plugin = cell(kept.version ? `${kept.plugin} v${kept.version}` : kept.plugin);
        tr.appendChild(plugin);
        const name = cell(kept.name);
        name.title = kept.id;
        tr.appendChild(name);
        tr.appendChild(cell(fmt(kept.at), 'element-time'));
        const parts = document.createElement('td');
        parts.appendChild(templateParts(kept));
        tr.appendChild(parts);
        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    s.appendChild(table);
    container.appendChild(s);
}

/** Sent, or refused and what the transport said. */
function outcome(m: MailRow): HTMLTableCellElement {
    const td = document.createElement('td');
    const pill = document.createElement('span');
    if (m.sent) {
        pill.textContent = 'sent';
        pill.className = 'element-pill element-pill-on';
        td.appendChild(pill);
        const id = document.createElement('span');
        id.className = 'element-pill-when';
        id.textContent = m.message_id;
        td.appendChild(id);
        return td;
    }
    pill.textContent = 'refused';
    pill.className = 'element-pill element-pill-off';
    td.appendChild(pill);
    const why = document.createElement('div');
    why.className = 'element-error';
    why.textContent = m.error;
    td.appendChild(why);
    return td;
}

/** Exported for tests: every mail sent or refused, newest first. */
export function renderSent(container: HTMLElement, mails: MailRow[]): void {
    container.innerHTML = '';
    const s = section('Sent');

    if (mails.length === 0) {
        s.appendChild(said('No mail sent yet.'));
        container.appendChild(s);
        return;
    }

    const table = document.createElement('table');
    table.className = 'element-table mail-sent-table';
    const thead = document.createElement('thead');
    thead.innerHTML = '<tr><th>When</th><th>User</th><th>To</th><th>Plugin</th><th>Template</th><th>Subject</th><th>Outcome</th></tr>';
    table.appendChild(thead);
    const tbody = document.createElement('tbody');
    for (const m of mails) {
        const tr = document.createElement('tr');
        const when = cell(fmt(m.at), 'element-time');
        // The attestation is the mail's record, and its id is on the row.
        when.title = m.id;
        tr.appendChild(when);
        tr.appendChild(cell(m.user));
        tr.appendChild(cell(m.to));
        tr.appendChild(cell(m.plugin));
        tr.appendChild(cell(m.template));
        tr.appendChild(cell(m.subject));
        tr.appendChild(outcome(m));
        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    s.appendChild(table);
    container.appendChild(s);
}

/** A section the node did not answer for says what it said instead, and the
 *  other sections still draw. */
function refused(container: HTMLElement, what: string, err: unknown): void {
    const message = `the node did not say ${what}: ${err instanceof Error ? err.message : String(err)}`;
    log.error(SEG.UI, `[MailElement] ${message}`, err);
    container.innerHTML = '';
    const box = document.createElement('div');
    box.className = 'element-error';
    box.textContent = message;
    container.appendChild(box);
}

function load(account: HTMLElement, templates: HTMLElement, sent: HTMLElement): void {
    apiJson<MailAccount>('/api/mail/account')
        .then((a) => renderAccount(account, a))
        .catch((err: unknown) => refused(account, 'what it sends mail through', err));
    apiJson<MailTemplates>('/api/mail/templates')
        .then((t) => renderTemplates(templates, t))
        .catch((err: unknown) => refused(templates, 'which templates mail is filled from', err));
    apiJson<{ mails: MailRow[] }>('/api/mail')
        .then((r) => renderSent(sent, r.mails))
        .catch((err: unknown) => refused(sent, 'what mail it sent', err));
}

export function createMailElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Mail',
        symbol: '✉',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'mail-element-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const account = document.createElement('div');
            const templates = document.createElement('div');
            const sent = document.createElement('div');
            for (const [part, what] of [[account, 'account'], [templates, 'templates'], [sent, 'sent mail']] as const) {
                part.appendChild(said(`Loading ${what}…`));
                content.appendChild(part);
            }

            load(account, templates, sent);
            return content;
        },
    };
}

/** Opens the Mail element. Called from ⍟, for ROOT. */
export function openMailElement(): void {
    tray.open(ELEMENT_ID);
}
