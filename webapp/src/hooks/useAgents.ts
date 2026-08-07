import {useCallback, useEffect, useRef, useState} from 'react';

import Client, {errorMessage, isNotConfiguredError} from '../client';
import type {Agent} from '../types';
import {normalizeAgentList} from '../utils/normalize';

const POLL_INTERVAL_MS = 30 * 1000;
const PAGE_SIZE = 100;

export interface AgentsState {
    agents: Agent[];
    loading: boolean;
    refreshing: boolean;
    error: string;
    refresh: () => void;
}

/**
 * Loads the user's agents and keeps them fresh while the panel is open.
 * `includeArchived` is always sent explicitly because upstream defaults it to
 * true.
 */
export function useAgents(includeArchived: boolean, onNotConfigured: () => void): AgentsState {
    const [agents, setAgents] = useState<Agent[]>([]);
    const [loading, setLoading] = useState(true);
    const [refreshing, setRefreshing] = useState(false);
    const [error, setError] = useState('');

    const mounted = useRef(true);
    const inFlight = useRef(false);

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
        };
    }, []);

    const load = useCallback(async (background: boolean) => {
        if (inFlight.current) {
            return;
        }
        inFlight.current = true;
        if (background) {
            setRefreshing(true);
        }

        try {
            const payload = await Client.listAgents({limit: PAGE_SIZE, includeArchived});
            if (!mounted.current) {
                return;
            }
            setAgents(normalizeAgentList(payload).agents);
            setError('');
        } catch (err) {
            if (!mounted.current) {
                return;
            }
            if (isNotConfiguredError(err)) {
                onNotConfigured();
            } else {
                setError(errorMessage(err, 'Could not load your Cloud Agents.'));
            }
        } finally {
            inFlight.current = false;
            if (mounted.current) {
                setLoading(false);
                setRefreshing(false);
            }
        }
    }, [includeArchived, onNotConfigured]);

    useEffect(() => {
        load(false);

        const timer = window.setInterval(() => load(true), POLL_INTERVAL_MS);
        const onFocus = () => load(true);
        window.addEventListener('focus', onFocus);

        return () => {
            window.clearInterval(timer);
            window.removeEventListener('focus', onFocus);
        };
    }, [load]);

    const refresh = useCallback(() => load(true), [load]);

    return {agents, loading, refreshing, error, refresh};
}
