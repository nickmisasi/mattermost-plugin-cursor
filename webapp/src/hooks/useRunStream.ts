import {useEffect, useRef, useState} from 'react';

import Client from '../client';
import type {RunStatus} from '../types';
import {RUN_STATUSES} from '../types';

export interface RunStreamState {
    text: string;
    activity: string;
    status?: RunStatus;
    error: string;
    streaming: boolean;
}

const EMPTY_STATE: RunStreamState = {text: '', activity: '', status: undefined, error: '', streaming: false};

function parseData(raw: string): unknown {
    try {
        return JSON.parse(raw) as unknown;
    } catch {
        return raw;
    }
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function extractText(data: unknown): string {
    if (typeof data === 'string') {
        return data;
    }
    if (!isRecord(data)) {
        return '';
    }
    for (const key of ['text', 'delta', 'content', 'message']) {
        const value = data[key];
        if (typeof value === 'string' && value) {
            return value;
        }
    }
    return '';
}

function extractStatus(data: unknown): RunStatus | undefined {
    if (!isRecord(data)) {
        return undefined;
    }
    const candidate = String(data.status ?? '').toUpperCase() as RunStatus;
    return RUN_STATUSES.includes(candidate) ? candidate : undefined;
}

function describeToolCall(data: unknown): string {
    if (!isRecord(data)) {
        return 'Running a tool';
    }
    const name = typeof data.name === 'string' ? data.name : 'tool';
    return data.status === 'completed' ? `Finished ${name}` : `Running ${name}`;
}

/**
 * Subscribes to a run's SSE stream and accumulates the assistant text emitted
 * since subscribing. `onFinished` fires once the run reports done or errors.
 */
export function useRunStream(agentId: string, runId: string, enabled: boolean, onFinished: () => void): RunStreamState {
    const [state, setState] = useState<RunStreamState>(EMPTY_STATE);
    const finishedRef = useRef(onFinished);

    useEffect(() => {
        finishedRef.current = onFinished;
    }, [onFinished]);

    useEffect(() => {
        if (!enabled || !agentId || !runId || typeof EventSource === 'undefined') {
            setState(EMPTY_STATE);
            return undefined;
        }

        setState({...EMPTY_STATE, streaming: true});

        const source = new EventSource(Client.streamUrl(agentId, runId), {withCredentials: true});
        let closed = false;

        const close = () => {
            if (!closed) {
                closed = true;
                source.close();
            }
        };

        const finish = () => {
            close();
            setState((prev) => ({...prev, activity: '', streaming: false}));
            finishedRef.current();
        };

        const listeners: Array<[string, (event: MessageEvent) => void]> = [
            ['status', (event) => {
                const status = extractStatus(parseData(event.data));
                setState((prev) => ({...prev, status: status ?? prev.status}));
            }],
            ['assistant', (event) => {
                const chunk = extractText(parseData(event.data));
                if (chunk) {
                    setState((prev) => ({...prev, text: prev.text + chunk, activity: ''}));
                }
            }],
            ['thinking', () => {
                setState((prev) => ({...prev, activity: 'Thinking…'}));
            }],
            ['tool_call', (event) => {
                const activity = describeToolCall(parseData(event.data));
                setState((prev) => ({...prev, activity}));
            }],
            ['result', (event) => {
                const data = parseData(event.data);
                const text = extractText(data);
                const status = extractStatus(data);
                setState((prev) => ({
                    ...prev,
                    text: text || prev.text,
                    status: status ?? prev.status,
                    activity: '',
                }));
            }],
            ['error', (event) => {
                const message = extractText(parseData(event.data)) || 'The agent stream reported an error.';
                setState((prev) => ({...prev, error: message, activity: '', streaming: false}));
                finish();
            }],
            ['done', () => finish()],
        ];

        for (const [name, handler] of listeners) {
            source.addEventListener(name, handler as EventListener);
        }

        // A transport-level failure (not the `error` SSE event) also ends the stream.
        source.onerror = () => {
            if (source.readyState === EventSource.CLOSED) {
                finish();
            }
        };

        return () => {
            for (const [name, handler] of listeners) {
                source.removeEventListener(name, handler as EventListener);
            }
            close();
        };
    }, [agentId, runId, enabled]);

    return state;
}
