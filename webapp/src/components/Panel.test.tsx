import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import React from 'react';

import Panel from './Panel';

import Client from '../client';

const repos = [{url: 'https://github.com/acme/bifrost', startingRef: 'main'}];

function listItem(status: string) {
    return {
        id: 'bc-1',
        name: 'Add README',
        status,
        env: {type: 'cloud'},
        url: 'https://cursor.com/agents/bc-1',
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        latestRunId: 'run-1',
        repos,
        runStatus: 'FINISHED',
    };
}

describe('Panel', () => {
    beforeEach(() => {
        jest.spyOn(Client, 'getKeyStatus').mockResolvedValue({configured: true, email: 'dev@example.com'});
        jest.spyOn(Client, 'getMessages').mockResolvedValue({id: 'bc-1', messages: []});
        jest.spyOn(Client, 'getRun').mockResolvedValue({id: 'run-1', agentId: 'bc-1', status: 'FINISHED'});
    });

    it('leaves the list without the agent after it is archived from the detail view', async () => {
        jest.spyOn(Client, 'getAgent').mockResolvedValue(listItem('ACTIVE'));
        jest.spyOn(Client, 'archiveAgent').mockResolvedValue(undefined);
        const listAgents = jest.spyOn(Client, 'listAgents').
            mockResolvedValueOnce({items: [listItem('ACTIVE')]}).
            mockResolvedValue({items: []});

        render(<Panel/>);

        fireEvent.click(await screen.findByText('Add README'));

        await screen.findByRole('button', {name: 'Agent actions'});
        jest.spyOn(Client, 'getAgent').mockResolvedValue(listItem('ARCHIVED'));
        fireEvent.click(screen.getByRole('button', {name: 'Agent actions'}));
        fireEvent.click(screen.getByRole('button', {name: 'Archive agent'}));

        await waitFor(() => expect(Client.archiveAgent).toHaveBeenCalledWith('bc-1'));

        // The detail view keeps reporting the last run and adds the lifecycle.
        expect(await screen.findByText('Archived')).toBeInTheDocument();
        expect(screen.getByText('Finished')).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', {name: 'Back to agents'}));

        expect(await screen.findByText('You have no Cloud Agents yet.')).toBeInTheDocument();
        expect(listAgents).toHaveBeenLastCalledWith({limit: 100, includeArchived: false});
    });
});
