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
    loadingMore: boolean;
    nextCursor: string;
    error: string;
    refresh: () => void;
    loadMore: () => void;
}

function appendUnique(existing: Agent[], incoming: Agent[]): Agent[] {
    const seen = new Set(existing.map((agent) => agent.id));
    return [...existing, ...incoming.filter((agent) => !seen.has(agent.id))];
}

/**
 * Loads the user's agents and keeps them fresh while the panel is open.
 *
 * Archived agents are always fetched and filtered during rendering, so the
 * archived toggle is instant and can never leave a request for the previous
 * filter in flight. `includeArchived` is still sent explicitly because
 * upstream defaults it to true.
 */
export function useAgents(onNotConfigured: () => void): AgentsState {
    const [agents, setAgents] = useState<Agent[]>([]);
    const [nextCursor, setNextCursor] = useState('');
    const [loading, setLoading] = useState(true);
    const [refreshing, setRefreshing] = useState(false);
    const [loadingMore, setLoadingMore] = useState(false);
    const [error, setError] = useState('');

    const mounted = useRef(true);
    const inFlight = useRef(false);

    // Bumped by every first-page load. A response may only write state if its
    // epoch is still the newest, so a slow reply can never clobber fresher data
    // or append a page that belongs to a superseded list.
    const epoch = useRef(0);

    // Once the user has paged past the first page, refreshes merge into the
    // list instead of truncating it back, and leave the cursor where it is.
    const hasPaged = useRef(false);

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
        };
    }, []);

    const handleError = useCallback((err: unknown) => {
        if (isNotConfiguredError(err)) {
            onNotConfigured();
        } else {
            setError(errorMessage(err, 'Could not load your Cloud Agents.'));
        }
    }, [onNotConfigured]);

    const loadFirstPage = useCallback(async (background: boolean) => {
        if (inFlight.current) {
            return;
        }
        inFlight.current = true;
        epoch.current += 1;
        const requestEpoch = epoch.current;

        if (background) {
            setRefreshing(true);
        }

        try {
            const payload = await Client.listAgents({limit: PAGE_SIZE, includeArchived: true});
            if (!mounted.current || requestEpoch !== epoch.current) {
                return;
            }
            const page = normalizeAgentList(payload);
            setAgents((current) => (hasPaged.current ? appendUnique(page.agents, current) : page.agents));
            if (!hasPaged.current) {
                setNextCursor(page.nextCursor);
            }
            setError('');
        } catch (err) {
            if (mounted.current && requestEpoch === epoch.current) {
                handleError(err);
            }
        } finally {
            inFlight.current = false;
            if (mounted.current) {
                setLoading(false);
                setRefreshing(false);
            }
        }
    }, [handleError]);

    const loadMore = useCallback(async () => {
        if (!nextCursor || inFlight.current) {
            return;
        }
        inFlight.current = true;
        const requestEpoch = epoch.current;
        setLoadingMore(true);

        try {
            const payload = await Client.listAgents({limit: PAGE_SIZE, includeArchived: true, cursor: nextCursor});
            if (!mounted.current || requestEpoch !== epoch.current) {
                return;
            }
            const page = normalizeAgentList(payload);
            hasPaged.current = true;
            setAgents((current) => appendUnique(current, page.agents));
            setNextCursor(page.nextCursor);
            setError('');
        } catch (err) {
            if (mounted.current && requestEpoch === epoch.current) {
                handleError(err);
            }
        } finally {
            inFlight.current = false;
            if (mounted.current) {
                setLoadingMore(false);
            }
        }
    }, [handleError, nextCursor]);

    useEffect(() => {
        loadFirstPage(false);

        const timer = window.setInterval(() => loadFirstPage(true), POLL_INTERVAL_MS);
        const onFocus = () => loadFirstPage(true);
        window.addEventListener('focus', onFocus);

        return () => {
            window.clearInterval(timer);
            window.removeEventListener('focus', onFocus);
        };
    }, [loadFirstPage]);

    const refresh = useCallback(() => loadFirstPage(true), [loadFirstPage]);

    return {agents, loading, refreshing, loadingMore, nextCursor, error, refresh, loadMore};
}
