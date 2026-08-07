import {useCallback, useEffect, useMemo, useRef, useState} from 'react';

import Client, {errorMessage, isNotConfiguredError} from '../client';
import type {Agent, ConversationMessage, Run} from '../types';
import {isActiveRunStatus} from '../types';
import {withRunResult} from '../utils/conversation';
import {normalizeAgent, normalizeMessages, normalizeRun} from '../utils/normalize';

export interface AgentDetailState {
    agent: Agent | null;
    run: Run | null;
    messages: ConversationMessage[];
    loading: boolean;
    error: string;
    reload: () => void;
}

/**
 * Loads a single agent along with its conversation. The latest run is fetched
 * separately because `GET /agents/{id}` carries only `latestRunId` — the
 * execution status, branch, PR and final result all live on the run.
 */
export function useAgentDetail(agentId: string, onNotConfigured: () => void): AgentDetailState {
    const [agent, setAgent] = useState<Agent | null>(null);
    const [run, setRun] = useState<Run | null>(null);
    const [fetchedMessages, setFetchedMessages] = useState<ConversationMessage[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');

    const mounted = useRef(true);

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
        };
    }, []);

    const load = useCallback(async () => {
        try {
            const [agentPayload, messagePayload] = await Promise.all([
                Client.getAgent(agentId),
                Client.getMessages(agentId).catch(() => null),
            ]);

            if (!mounted.current) {
                return;
            }

            const nextAgent = normalizeAgent(agentPayload);
            setAgent(nextAgent);
            setFetchedMessages(normalizeMessages(messagePayload));
            setError('');

            if (nextAgent?.latestRunId) {
                const runPayload = await Client.getRun(agentId, nextAgent.latestRunId).catch(() => null);
                if (mounted.current) {
                    setRun(normalizeRun(runPayload));
                }
            } else {
                setRun(null);
            }
        } catch (err) {
            if (!mounted.current) {
                return;
            }
            if (isNotConfiguredError(err)) {
                onNotConfigured();
            } else {
                setError(errorMessage(err, 'Could not load this agent.'));
            }
        } finally {
            if (mounted.current) {
                setLoading(false);
            }
        }
    }, [agentId, onNotConfigured]);

    useEffect(() => {
        setLoading(true);
        load();
    }, [load]);

    const reload = useCallback(() => load(), [load]);
    const messages = useMemo(() => withRunResult(fetchedMessages, run), [fetchedMessages, run]);

    return {agent, run, messages, loading, error, reload};
}

export function resolveRunStatus(agent: Agent | null, run: Run | null) {
    return run?.status ?? agent?.runStatus;
}

export function hasActiveRun(agent: Agent | null, run: Run | null): boolean {
    return isActiveRunStatus(resolveRunStatus(agent, run));
}
