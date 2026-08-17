// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {Store, Action} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import HealthURLDisplay from '@/components/health_url_display';
import WebhookURLDisplay from '@/components/webhook_url_display';
import manifest from '@/manifest';
import type {PluginRegistry} from '@/types/mattermost-webapp';

export default class Plugin {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    public async initialize(registry: PluginRegistry, store: Store<GlobalState, Action<Record<string, unknown>>>) {
        // Register custom sections in the System Console plugin settings page that
        // render the full webhook and uptime check URLs with a copy button.
        registry.registerAdminConsoleCustomSection('webhook-url-display', WebhookURLDisplay);
        registry.registerAdminConsoleCustomSection('health-url-display', HealthURLDisplay);
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
