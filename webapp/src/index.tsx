import type {AnyAction, Store} from 'redux';

import Panel from './components/Panel';
import manifest from './manifest';
import type {PluginRegistry} from './types/mattermost-webapp';

import './components/cursor.css';

const RHS_TITLE = 'Cursor Agents';
const APP_BAR_ICON = `/plugins/${manifest.id}/public/app-bar-icon.png`;

export default class Plugin {
    public initialize(registry: PluginRegistry, store: Store<unknown, AnyAction>): void {
        const {toggleRHSPlugin} = registry.registerRightHandSidebarComponent(Panel, RHS_TITLE);

        registry.registerAppBarComponent(
            APP_BAR_ICON,
            () => store.dispatch(toggleRHSPlugin),
            RHS_TITLE,
            null,
        );
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
