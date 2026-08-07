import {fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import React from 'react';

import AgentList from './AgentList';
import AgentListContainer from './AgentListContainer';

import Client, {API_KEY_NOT_CONFIGURED, ClientError} from '../client';
import {makeAgent} from '../testing/fixtures';
import type {Agent} from '../types';

const agent = makeAgent;

function renderList(agents: Agent[], overrides: Partial<React.ComponentProps<typeof AgentList>> = {}) {
    return render(
        <AgentList
            agents={agents}
            loading={false}
            refreshing={false}
            loadingMore={false}
            hasMore={false}
            error=''
            email='dev@example.com'
            onRefresh={jest.fn()}
            onLoadMore={jest.fn()}
            onNewAgent={jest.fn()}
            onSelectAgent={jest.fn()}
            onOpenSettings={jest.fn()}
            {...overrides}
        />,
    );
}

describe('AgentList', () => {
    it('renders agents grouped under their repository with the branch and relative time', () => {
        renderList([
            agent({id: 'a', name: 'Bedrock custom endpoint support', branch: 'cursor/bedrock-custom-endpoints-9284', activityAt: Date.now() - (9 * 60 * 60 * 1000)}),
            agent({id: 'b', name: 'Retry flaky test', repositoryUrl: 'https://github.com/mattermost/factory', repository: 'mattermost/factory', activityAt: Date.now()}),
        ]);

        expect(screen.getByRole('heading', {name: /mattermost\/factory/})).toBeInTheDocument();
        expect(screen.getByText('Bedrock custom endpoint support')).toBeInTheDocument();
        expect(screen.getByText('cursor/bedrock-custom-endpoints-9284')).toBeInTheDocument();
        expect(screen.getByText('9h')).toBeInTheDocument();
        expect(screen.getByText('dev@example.com')).toBeInTheDocument();
    });

    it('collapses groups past three agents behind a More row', () => {
        renderList([1, 2, 3, 4, 5].map((n) => agent({id: `a${n}`, name: `Agent ${n}`, activityAt: n})));

        expect(screen.queryByText('Agent 1')).not.toBeInTheDocument();
        expect(screen.getByRole('button', {name: 'More (2)'})).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', {name: 'More (2)'}));

        expect(screen.getByText('Agent 1')).toBeInTheDocument();
        expect(screen.queryByRole('button', {name: 'More (2)'})).not.toBeInTheDocument();
    });

    it('filters agents through the search box', () => {
        renderList([
            agent({id: 'a', name: 'Bedrock support'}),
            agent({id: 'b', name: 'Retry flaky test'}),
        ]);

        fireEvent.click(screen.getByRole('button', {name: 'Search'}));
        fireEvent.change(screen.getByLabelText('Search agents'), {target: {value: 'flaky'}});

        expect(screen.getByText('Retry flaky test')).toBeInTheDocument();
        expect(screen.queryByText('Bedrock support')).not.toBeInTheDocument();
    });

    it('toggles archived agents client side without refetching', () => {
        const onRefresh = jest.fn();
        renderList([
            agent({id: 'a', name: 'Old work', archived: true}),
            agent({id: 'b', name: 'Current work'}),
        ], {onRefresh});

        expect(screen.queryByText('Old work')).not.toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', {name: 'Show archived agents'}));

        expect(screen.getByText('Old work')).toBeInTheDocument();
        expect(screen.getByText('Current work')).toBeInTheDocument();
        expect(onRefresh).not.toHaveBeenCalled();

        fireEvent.click(screen.getByRole('button', {name: 'Hide archived agents'}));

        expect(screen.queryByText('Old work')).not.toBeInTheDocument();
    });

    it('clears the query when the search box is closed again', () => {
        renderList([agent({id: 'a', name: 'Bedrock support'})]);

        fireEvent.click(screen.getByRole('button', {name: 'Search'}));
        fireEvent.change(screen.getByLabelText('Search agents'), {target: {value: 'nothing matches'}});
        expect(screen.queryByText('Bedrock support')).not.toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', {name: 'Close search'}));

        expect(screen.getByText('Bedrock support')).toBeInTheDocument();
    });

    it('offers Load more only when another page exists', () => {
        const onLoadMore = jest.fn();
        renderList([agent({id: 'a'})], {onLoadMore});
        expect(screen.queryByRole('button', {name: 'Load more'})).not.toBeInTheDocument();

        renderList([agent({id: 'a'})], {hasMore: true, onLoadMore});
        fireEvent.click(screen.getAllByRole('button', {name: 'Load more'})[0]);

        expect(onLoadMore).toHaveBeenCalled();
    });

    it('disables Load more while the next page is in flight', () => {
        renderList([agent({id: 'a'})], {hasMore: true, loadingMore: true});

        expect(screen.getByRole('button', {name: 'Loading…'})).toBeDisabled();
    });

    it('invites the user to launch their first agent when the list is empty', () => {
        const onNewAgent = jest.fn();
        renderList([], {onNewAgent});

        fireEvent.click(screen.getByRole('button', {name: 'Launch your first agent'}));
        expect(onNewAgent).toHaveBeenCalled();
    });

    it('selects an agent when its row is clicked', () => {
        const onSelectAgent = jest.fn();
        renderList([agent({id: 'bc-7', name: 'Bedrock support'})], {onSelectAgent});

        fireEvent.click(screen.getByText('Bedrock support'));
        expect(onSelectAgent).toHaveBeenCalledWith('bc-7');
    });
});

function renderContainer(onNotConfigured: () => void = jest.fn()) {
    return render(
        <AgentListContainer
            email='dev@example.com'
            onNewAgent={jest.fn()}
            onSelectAgent={jest.fn()}
            onOpenSettings={jest.fn()}
            onNotConfigured={onNotConfigured}
        />,
    );
}

describe('AgentListContainer', () => {
    it('renders the enriched agents returned by the plugin API', async () => {
        jest.spyOn(Client, 'listAgents').mockResolvedValue({
            items: [{
                id: 'bc-1',
                name: 'Add README',
                status: 'ACTIVE',
                env: {type: 'cloud'},
                url: 'https://cursor.com/agents/bc-1',
                createdAt: new Date().toISOString(),
                updatedAt: new Date().toISOString(),
                latestRunId: 'run-1',
                repos: [{url: 'https://github.com/acme/bifrost', startingRef: 'main'}],
                branch: 'cursor/add-readme-a1b2',
                prUrl: 'https://github.com/acme/bifrost/pull/1',
                runStatus: 'RUNNING',
            }],
        });

        renderContainer();

        expect(screen.getByText('Loading your Cloud Agents…')).toBeInTheDocument();

        const group = await screen.findByRole('heading', {name: /acme\/bifrost/});
        const rendered = within(group.parentElement as HTMLElement);
        expect(rendered.getByText('Add README')).toBeInTheDocument();
        expect(rendered.getByText('cursor/add-readme-a1b2')).toBeInTheDocument();
        expect(rendered.getByRole('img', {name: 'Running'})).toBeInTheDocument();
    });

    it('always fetches archived agents so the filter never refetches', async () => {
        jest.spyOn(Client, 'listAgents').mockResolvedValue({
            items: [{id: 'bc-1', name: 'Old work', status: 'ARCHIVED', repos: [{url: 'https://github.com/acme/bifrost'}]}],
        });

        renderContainer();

        await waitFor(() => expect(Client.listAgents).toHaveBeenCalledWith({limit: 100, includeArchived: true}));
        expect(screen.queryByText('Old work')).not.toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', {name: 'Show archived agents'}));

        expect(screen.getByText('Old work')).toBeInTheDocument();
        expect(Client.listAgents).toHaveBeenCalledTimes(1);
    });

    it('appends the next page and stops offering Load more when the cursor runs out', async () => {
        const page = (id: string, nextCursor?: string) => ({
            items: [{id, name: `Agent ${id}`, status: 'ACTIVE', repos: [{url: 'https://github.com/acme/bifrost'}]}],
            ...(nextCursor ? {nextCursor} : {}),
        });
        jest.spyOn(Client, 'listAgents').
            mockResolvedValueOnce(page('bc-1', 'cursor-2')).
            mockResolvedValueOnce(page('bc-2'));

        renderContainer();

        fireEvent.click(await screen.findByRole('button', {name: 'Load more'}));

        expect(await screen.findByText('Agent bc-2')).toBeInTheDocument();
        expect(screen.getByText('Agent bc-1')).toBeInTheDocument();
        expect(Client.listAgents).toHaveBeenLastCalledWith({limit: 100, includeArchived: true, cursor: 'cursor-2'});
        await waitFor(() => expect(screen.queryByRole('button', {name: 'Load more'})).not.toBeInTheDocument());
    });

    it('does not duplicate agents that the next page repeats', async () => {
        const items = [{id: 'bc-1', name: 'Agent bc-1', status: 'ACTIVE', repos: [{url: 'https://github.com/acme/bifrost'}]}];
        jest.spyOn(Client, 'listAgents').
            mockResolvedValueOnce({items, nextCursor: 'cursor-2'}).
            mockResolvedValueOnce({items});

        renderContainer();

        fireEvent.click(await screen.findByRole('button', {name: 'Load more'}));

        await waitFor(() => expect(Client.listAgents).toHaveBeenCalledTimes(2));
        expect(screen.getAllByText('Agent bc-1')).toHaveLength(1);
    });

    it('asks the panel to show setup when the API key is gone', async () => {
        const onNotConfigured = jest.fn();
        jest.spyOn(Client, 'listAgents').mockRejectedValue(
            new ClientError(403, 'Connect your Cursor account', API_KEY_NOT_CONFIGURED),
        );

        renderContainer(onNotConfigured);

        await waitFor(() => expect(onNotConfigured).toHaveBeenCalled());
        expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });

    it('shows a banner for other failures', async () => {
        jest.spyOn(Client, 'listAgents').mockRejectedValue(new ClientError(500, 'Cursor is unavailable.'));

        renderContainer();

        expect(await screen.findByRole('alert')).toHaveTextContent('Cursor is unavailable.');
    });
});
