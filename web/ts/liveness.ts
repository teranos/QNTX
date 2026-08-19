// What the browser observed asking one question. Why the answer was that is
// not something this side of the wire knows, so nothing here says why.
export interface Reached {
    url: string;
    // 0 when no answer arrived at all.
    status: number;
    body: string;
    // The thrown message, verbatim, when the request did not complete.
    error: string;
}

export async function askHealth(url: string): Promise<Reached> {
    try {
        const response = await fetch(url);
        return {
            url,
            status: response.status,
            body: (await response.text()).trim(),
            error: '',
        };
    } catch (thrown: unknown) {
        return {
            url,
            status: 0,
            body: '',
            error: thrown instanceof Error ? `${thrown.name}: ${thrown.message}` : String(thrown),
        };
    }
}

export function isLive(reached: Reached): boolean {
    return reached.status === 200;
}

// One line per observation, each of them something that happened rather than
// something concluded from it. A reader can check every one against the wire.
export function statedPlainly(reached: Reached): string[] {
    const said: string[] = [`GET ${reached.url}`];

    if (reached.status === 0) {
        said.push(`no answer — ${reached.error}`);
    } else {
        said.push(`answered ${reached.status}`);
    }
    if (reached.body !== '') {
        said.push(`the node said: ${reached.body}`);
    }

    said.push('QNTX did not start. Nothing below this line was loaded.');
    return said;
}
