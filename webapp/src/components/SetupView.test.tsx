import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import React from 'react';

import SetupView from './SetupView';

import Client, {ClientError} from '../client';

describe('SetupView', () => {
    it('explains the connection and disables Connect until a key is typed', () => {
        render(
            <SetupView
                mode='connect'
                onConnected={jest.fn()}
            />,
        );

        expect(screen.getByText('Connect your Cursor account')).toBeInTheDocument();
        expect(screen.getByRole('link', {name: 'https://cursor.com/dashboard'})).
            toHaveAttribute('href', 'https://cursor.com/dashboard');

        const connect = screen.getByRole('button', {name: 'Connect'});
        expect(connect).toBeDisabled();

        fireEvent.change(screen.getByLabelText('Cursor API key'), {target: {value: 'key_123'}});
        expect(connect).toBeEnabled();
    });

    it('masks the API key input', () => {
        render(
            <SetupView
                mode='connect'
                onConnected={jest.fn()}
            />,
        );

        expect(screen.getByLabelText('Cursor API key')).toHaveAttribute('type', 'password');
    });

    it('reports the connected email once the key is accepted', async () => {
        const onConnected = jest.fn();
        jest.spyOn(Client, 'setKey').mockResolvedValue({configured: true, email: 'dev@example.com'});

        render(
            <SetupView
                mode='connect'
                onConnected={onConnected}
            />,
        );

        fireEvent.change(screen.getByLabelText('Cursor API key'), {target: {value: ' key_123 '}});
        fireEvent.click(screen.getByRole('button', {name: 'Connect'}));

        await waitFor(() => expect(onConnected).toHaveBeenCalledWith('dev@example.com'));
        expect(Client.setKey).toHaveBeenCalledWith('key_123');
    });

    it('shows the server message when the key is rejected', async () => {
        jest.spyOn(Client, 'setKey').mockRejectedValue(new ClientError(400, 'That API key is not valid.'));

        render(
            <SetupView
                mode='connect'
                onConnected={jest.fn()}
            />,
        );

        fireEvent.change(screen.getByLabelText('Cursor API key'), {target: {value: 'bad'}});
        fireEvent.click(screen.getByRole('button', {name: 'Connect'}));

        expect(await screen.findByRole('alert')).toHaveTextContent('That API key is not valid.');
    });

    it('offers disconnect in manage mode', async () => {
        const onDisconnected = jest.fn();
        jest.spyOn(Client, 'deleteKey').mockResolvedValue(undefined);

        render(
            <SetupView
                mode='manage'
                email='dev@example.com'
                onConnected={jest.fn()}
                onDisconnected={onDisconnected}
                onBack={jest.fn()}
            />,
        );

        expect(screen.getByText('Connected as dev@example.com')).toBeInTheDocument();
        fireEvent.click(screen.getByRole('button', {name: 'Disconnect'}));

        await waitFor(() => expect(onDisconnected).toHaveBeenCalled());
    });
});
