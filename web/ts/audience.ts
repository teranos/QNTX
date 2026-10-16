// "an Element should know about its intended audience"

// Its audience is whoever reaches every path it needs. The node says who does
// (`i reaches`); the table itself never reaches the browser.

import type { Element } from '@teranos/elements';
import { apiFetch } from './client/http';
import type { Reaches } from './generated/proto/plugin/grpc/protocol/user';

// At least one path: an element naming none would be every viewer's, which is meaning given to nothing.
export type Needs = readonly [string, ...string[]];

export interface Audience {
    needs: Needs;
    create: () => Element;
}

/** Of the paths asked, the ones the viewer reaches. */
export type Ask = (paths: string[]) => Promise<string[]>;

/** The elements whose every path the viewer reaches, asked of the node once. */
export async function forTheViewer(offered: readonly Audience[], ask: Ask): Promise<Audience[]> {
    const paths: string[] = [];
    for (const each of offered) {
        for (const path of each.needs) {
            if (!paths.includes(path)) paths.push(path);
        }
    }
    const reached = new Set(await ask(paths));
    return offered.filter(each => each.needs.every(path => reached.has(path)));
}

/** The node's answer, `GET /i/reaches`. A 401 is the gate saying the viewer is admitted nowhere it guards. */
export async function askTheNode(paths: string[]): Promise<string[]> {
    const path = `/i/reaches?paths=${encodeURIComponent(paths.join(','))}`;
    const response = await apiFetch(path);
    if (response.status === 401) return [];
    if (!response.ok) {
        throw new Error(`${path}: HTTP ${response.status} ${await response.text()}`);
    }
    const answered = await response.json() as Reaches;
    return answered.paths;
}
