import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';
import React from 'react';

import AgentDetailView from './AgentDetailView';

import Client, {API_KEY_NOT_CONFIGURED, ClientError} from '../client';

type Listener = (event: MessageEvent) => void;

/** jsdom has no EventSource, so the streaming tests install this in its place. */
class FakeEventSource {
    static readonly CLOSED = 2;

    static last: FakeEventSource | null = null;

    readyState = 1;

    onerror: (() => void) | null = null;

    private listeners = new Map<string, Listener[]>();

    constructor() {
        FakeEventSource.last = this;
    }

    addEventListener(name: string, handler: Listener) {
        this.listeners.set(name, [...(this.listeners.get(name) ?? []), handler]);
    }

    close() {
        this.readyState = FakeEventSource.CLOSED;
    }

    emit(name: string, data: unknown) {
        const event = {data: JSON.stringify(data)} as MessageEvent;
        act(() => {
            for (const handler of this.listeners.get(name) ?? []) {
                handler(event);
            }
        });
    }
}

function useFakeEventSource() {
    beforeEach(() => {
        FakeEventSource.last = null;
        (global as {EventSource?: unknown}).EventSource = FakeEventSource;
    });

    afterEach(() => {
        delete (global as {EventSource?: unknown}).EventSource;
    });
}

function mockAgent(latestRunStatus = 'FINISHED', run: Record<string, unknown> = {}) {
    jest.spyOn(Client, 'getAgent').mockResolvedValue({
        id: 'bc-1',
        name: 'Add README',
        status: 'ACTIVE',
        env: {type: 'cloud'},
        url: 'https://cursor.com/agents/bc-1',
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        latestRunId: 'run-1',
        repos: [{url: 'https://github.com/acme/bifrost', startingRef: 'main'}],
        workOnCurrentBranch: false,
        autoCreatePR: true,
    });
    jest.spyOn(Client, 'getRun').mockResolvedValue({
        id: 'run-1',
        agentId: 'bc-1',
        status: latestRunStatus,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        git: {branches: [{repoUrl: 'github.com/acme/bifrost', branch: 'cursor/add-readme-a1b2', prUrl: 'https://github.com/acme/bifrost/pull/1'}]},
        ...run,
    });
    jest.spyOn(Client, 'getMessages').mockResolvedValue({
        id: 'bc-1',
        messages: [
            {id: 'm1', type: 'user_message', text: 'Add a README'},
            {id: 'm2', type: 'assistant_message', text: 'Done.'},
        ],
    });
}

function renderDetail() {
    return render(
        <AgentDetailView
            agentId='bc-1'
            onBack={jest.fn()}
            onNotConfigured={jest.fn()}
            onDeleted={jest.fn()}
        />,
    );
}

describe('AgentDetailView', () => {
    it('shows the agent header, links and conversation', async () => {
        mockAgent();
        renderDetail();

        expect(await screen.findByText('Add README')).toBeInTheDocument();
        expect(screen.getByText('Finished')).toBeInTheDocument();
        expect(screen.getByText('acme/bifrost')).toBeInTheDocument();
        expect(screen.getByRole('link', {name: 'Pull request'})).
            toHaveAttribute('href', 'https://github.com/acme/bifrost/pull/1');
        expect(screen.getByRole('link', {name: 'Open in Cursor'})).
            toHaveAttribute('href', 'https://cursor.com/agents/bc-1');
        expect(screen.getByText('Add a README')).toBeInTheDocument();
        expect(screen.getByText('Done.')).toBeInTheDocument();
    });

    it('renders an active run without an EventSource available', async () => {
        mockAgent('RUNNING');
        renderDetail();

        expect(await screen.findByText('Running')).toBeInTheDocument();
        expect(screen.getByRole('button', {name: 'Agent actions'})).toBeEnabled();
    });

    it('shows the run duration and its final result when the conversation lags', async () => {
        mockAgent('FINISHED', {durationMs: 12_357, result: 'Added README.md with setup steps.'});
        jest.spyOn(Client, 'getMessages').mockResolvedValue({
            id: 'bc-1',
            messages: [{id: 'm1', type: 'user_message', text: 'Add a README'}],
        });
        renderDetail();

        expect(await screen.findByText('Added README.md with setup steps.')).toBeInTheDocument();
        expect(screen.getByText(/12s/)).toBeInTheDocument();
    });

    it('tells the user to wait when a follow-up conflicts with a running agent', async () => {
        mockAgent('RUNNING');
        jest.spyOn(Client, 'followup').mockRejectedValue(new ClientError(409, 'agent busy', 'agent_busy'));
        renderDetail();

        await screen.findByText('Add README');
        fireEvent.change(screen.getByPlaceholderText('Send a follow-up…'), {target: {value: 'also add tests'}});
        fireEvent.click(screen.getByRole('button', {name: 'Send follow-up'}));

        expect(await screen.findByRole('alert')).toHaveTextContent('Agent is still running.');
    });

    it('sends a follow-up and reloads the conversation', async () => {
        mockAgent();
        jest.spyOn(Client, 'followup').mockResolvedValue({id: 'run-2', status: 'CREATING'});
        renderDetail();

        await screen.findByText('Add README');
        fireEvent.change(screen.getByPlaceholderText('Send a follow-up…'), {target: {value: 'also add tests'}});
        fireEvent.click(screen.getByRole('button', {name: 'Send follow-up'}));

        await waitFor(() => expect(Client.followup).toHaveBeenCalledWith('bc-1', 'also add tests'));
        await waitFor(() => expect(Client.getAgent).toHaveBeenCalledTimes(2));
    });

    it('surfaces a load failure', async () => {
        jest.spyOn(Client, 'getAgent').mockRejectedValue(new ClientError(404, 'Agent not found.'));
        jest.spyOn(Client, 'getMessages').mockResolvedValue(null);
        renderDetail();

        expect(await screen.findByRole('alert')).toHaveTextContent('Agent not found.');
    });

    it('still renders when the conversation fetch fails', async () => {
        mockAgent();
        jest.spyOn(Client, 'getMessages').mockRejectedValue(new ClientError(500, 'upstream error'));
        renderDetail();

        expect(await screen.findByText('No conversation yet.')).toBeInTheDocument();
        expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });

    it('routes a lost API key from the conversation fetch to the panel', async () => {
        const onNotConfigured = jest.fn();
        mockAgent();
        jest.spyOn(Client, 'getMessages').mockRejectedValue(
            new ClientError(403, 'Connect your Cursor account', API_KEY_NOT_CONFIGURED),
        );

        render(
            <AgentDetailView
                agentId='bc-1'
                onBack={jest.fn()}
                onNotConfigured={onNotConfigured}
                onDeleted={jest.fn()}
            />,
        );

        await waitFor(() => expect(onNotConfigured).toHaveBeenCalled());
    });
});

describe('AgentDetailView streaming', () => {
    useFakeEventSource();

    it('flips the badge live and refetches once the run is done', async () => {
        mockAgent('RUNNING');
        renderDetail();

        expect(await screen.findByText('Running')).toBeInTheDocument();
        const source = FakeEventSource.last;
        expect(source).not.toBeNull();

        source?.emit('assistant', {text: 'Reading the README'});
        expect(screen.getByText('Reading the README')).toBeInTheDocument();

        source?.emit('tool_call', {callId: 'call-1', name: 'read_file', status: 'running'});
        expect(screen.getByText('Running read_file')).toBeInTheDocument();

        source?.emit('status', {runId: 'run-1', status: 'FINISHED'});
        expect(screen.getByText('Finished')).toBeInTheDocument();

        source?.emit('done', {});
        expect(source?.readyState).toBe(FakeEventSource.CLOSED);
        await waitFor(() => expect(Client.getAgent).toHaveBeenCalledTimes(2));
    });

    it('reports a stream error and stops streaming', async () => {
        mockAgent('RUNNING');
        renderDetail();

        await screen.findByText('Running');
        FakeEventSource.last?.emit('error', {text: 'The run could not be streamed.'});

        expect(await screen.findByRole('alert')).toHaveTextContent('The run could not be streamed.');
        expect(FakeEventSource.last?.readyState).toBe(FakeEventSource.CLOSED);
    });

    it('does not open a stream for a terminal run', async () => {
        mockAgent('FINISHED');
        renderDetail();

        await screen.findByText('Finished');
        expect(FakeEventSource.last).toBeNull();
    });
});
