import { describe, it, expect } from 'bun:test';
import { watchAction, findWatcher, type Watcher } from './handlers-panel';

function watcher(id: string, plugin: string, handler: string): Watcher {
    return {
        id,
        action_type: 'plugin_execute',
        action_data: JSON.stringify({ plugin_name: plugin, handler_name: handler }),
        fire_count: 48,
        error_count: 0,
    };
}

describe('what fires a handler', () => {
    it('reads the plugin and the handler the engine wrote', () => {
        const said = watchAction(JSON.stringify({ plugin_name: 'capy', handler_name: 'mp001' }));
        expect(said).toEqual({ plugin: 'capy', handler: 'mp001' });
    });

    it('finds the watcher whatever the watcher is called', () => {
        // The id used to be the join key, on the assumption that it starts with
        // the plugin name. Nothing in the engine promises that.
        const all = [watcher('pond-duck-7', 'capy', 'mp001_check_licensed_media')];

        expect(findWatcher(all, 'capy', 'mp001_check_licensed_media')).toBe(all[0]);
    });

    it('does not cross two plugins that share a handler name', () => {
        const all = [
            watcher('w-1', 'capy', 'render'),
            watcher('w-2', 'duif', 'render'),
        ];

        expect(findWatcher(all, 'duif', 'render')).toBe(all[1]);
    });

    it('matches nothing rather than guessing when the action is unreadable', () => {
        const broken: Watcher = {
            id: 'w-3',
            action_type: 'plugin_execute',
            action_data: '{ not json',
            fire_count: 0,
            error_count: 0,
        };

        expect(watchAction(broken.action_data)).toEqual({ plugin: '', handler: '' });
        expect(findWatcher([broken], 'capy', 'render')).toBeUndefined();
    });

    it('ignores watchers that are not plugin executions', () => {
        const all: Watcher[] = [{
            id: 'w-4',
            action_type: 'glyph_execute',
            action_data: JSON.stringify({ plugin_name: 'capy', handler_name: 'render' }),
            fire_count: 3,
            error_count: 0,
        }];

        expect(findWatcher(all, 'capy', 'render')).toBeUndefined();
    });
});
