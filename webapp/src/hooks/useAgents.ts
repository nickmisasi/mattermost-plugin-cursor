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
 * `includeArchived` is sent on every request because the server is the only
 * authority on which agents are archived: list items are documented as
 * carrying "only the durable identity fields", so a payload need not say that
 * an agent is archived and the client-side predicate in `groupAgentsByRepository`
 * cannot be the only thing hiding them. Rendering still applies that predicate
 * so the toggle responds without waiting for the refetch.
 */
export function useAgents(includeArchived: boolean, onNotConfigured: () => void): AgentsState {
    const [agents, setAgents] = useState<Agent[]>([]);
    const [nextCursor, setNextCursor] = useState('');
    const [loading, setLoading] = useState(true);
    const [refreshing, setRefreshing] = useState(false);
    const [loadingMore, setLoadingMore] = useState(false);
    const [error, setError] = useState('');

    const mounted = useRef(true);
    const loadedOnce = useRef(false);
    const inFlight = useRef(false);

    // Bumped by every first-page load. A response may only write state if its
    // epoch is still the newest, so a reply for the previous filter can never
    // clobber fresher data or append a page that belongs to a superseded list.
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

    const loadFirstPage = useCallback(async () => {
        epoch.current += 1;
        const requestEpoch = epoch.current;
        const current = () => mounted.current && requestEpoch === epoch.current;
        inFlight.current = true;

        if (loadedOnce.current) {
            setRefreshing(true);
        }

        try {
            const payload = await Client.listAgents({limit: PAGE_SIZE, includeArchived});
            if (!current()) {
                return;
            }
            const page = normalizeAgentList(payload);
            setAgents((existing) => (hasPaged.current ? appendUnique(page.agents, existing) : page.agents));
            if (!hasPaged.current) {
                setNextCursor(page.nextCursor);
            }
            setError('');
        } catch (err) {
            if (current()) {
                handleError(err);
            }
        } finally {
            loadedOnce.current = true;
            inFlight.current = false;
            if (current()) {
                setLoading(false);
                setRefreshing(false);
            }
        }
    }, [handleError, includeArchived]);

    // Polling and focus must never displace a load the user is waiting on; a
    // filter change or an explicit refresh always goes through.
    const backgroundRefresh = useCallback(() => {
        if (!inFlight.current) {
            loadFirstPage();
        }
    }, [loadFirstPage]);

    const loadMore = useCallback(async () => {
        if (!nextCursor || loadingMore) {
            return;
        }
        const requestEpoch = epoch.current;
        const current = () => mounted.current && requestEpoch === epoch.current;
        setLoadingMore(true);

        try {
            const payload = await Client.listAgents({limit: PAGE_SIZE, includeArchived, cursor: nextCursor});
            if (!current()) {
                return;
            }
            const page = normalizeAgentList(payload);
            hasPaged.current = true;
            setAgents((existing) => appendUnique(existing, page.agents));
            setNextCursor(page.nextCursor);
            setError('');
        } catch (err) {
            if (current()) {
                handleError(err);
            }
        } finally {
            if (mounted.current) {
                setLoadingMore(false);
            }
        }
    }, [handleError, includeArchived, loadingMore, nextCursor]);

    useEffect(() => {
        // Changing the filter asks for a different result set, so the pages
        // already collected no longer apply.
        hasPaged.current = false;
        loadFirstPage();

        const timer = window.setInterval(backgroundRefresh, POLL_INTERVAL_MS);
        window.addEventListener('focus', backgroundRefresh);

        return () => {
            window.clearInterval(timer);
            window.removeEventListener('focus', backgroundRefresh);
        };
    }, [backgroundRefresh, loadFirstPage]);

    return {agents, loading, refreshing, loadingMore, nextCursor, error, refresh: loadFirstPage, loadMore};
}
