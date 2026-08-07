import React, {useCallback, useEffect, useState} from 'react';

import AgentDetailView from './AgentDetailView';
import AgentListContainer from './AgentListContainer';
import NewAgentView from './NewAgentView';
import SetupView from './SetupView';

import Client, {errorMessage} from '../client';

type View =
    | {name: 'list'}
    | {name: 'new'}
    | {name: 'settings'}
    | {name: 'detail'; agentId: string};

const Panel = () => {
    const [checkingKey, setCheckingKey] = useState(true);
    const [configured, setConfigured] = useState(false);
    const [email, setEmail] = useState('');
    const [keyError, setKeyError] = useState('');
    const [view, setView] = useState<View>({name: 'list'});

    const loadKeyStatus = useCallback(async () => {
        setCheckingKey(true);
        setKeyError('');
        try {
            // A missing key is reported as `configured: false`, not as an error.
            const status = await Client.getKeyStatus();
            setConfigured(status.configured);
            setEmail(status.email);
        } catch (err) {
            setKeyError(errorMessage(err, 'Could not check your Cursor connection.'));
        } finally {
            setCheckingKey(false);
        }
    }, []);

    useEffect(() => {
        loadKeyStatus();
    }, [loadKeyStatus]);

    const handleNotConfigured = useCallback(() => {
        setConfigured(false);
        setEmail('');
        setView({name: 'list'});
    }, []);

    const handleConnected = useCallback((connectedEmail: string) => {
        setConfigured(true);
        setEmail(connectedEmail);
        setView({name: 'list'});
    }, []);

    const showList = useCallback(() => setView({name: 'list'}), []);

    if (checkingKey) {
        return (
            <div className='cursor-panel'>
                <p className='cursor-placeholder'>{'Checking your Cursor connection…'}</p>
            </div>
        );
    }

    if (keyError) {
        return (
            <div className='cursor-panel cursor-panel--centered'>
                <p
                    className='cursor-error'
                    role='alert'
                >
                    {keyError}
                </p>
                <button
                    type='button'
                    className='btn btn-primary'
                    onClick={() => loadKeyStatus()}
                >
                    {'Try again'}
                </button>
            </div>
        );
    }

    if (!configured) {
        return (
            <SetupView
                mode='connect'
                onConnected={handleConnected}
            />
        );
    }

    switch (view.name) {
    case 'settings':
        return (
            <SetupView
                mode='manage'
                email={email}
                onConnected={handleConnected}
                onDisconnected={handleNotConfigured}
                onBack={showList}
            />
        );
    case 'new':
        return (
            <NewAgentView
                onCancel={showList}
                onCreated={(agentId) => setView({name: 'detail', agentId})}
                onNotConfigured={handleNotConfigured}
            />
        );
    case 'detail':
        return (
            <AgentDetailView
                key={view.agentId}
                agentId={view.agentId}
                onBack={showList}
                onNotConfigured={handleNotConfigured}
                onDeleted={showList}
            />
        );
    default:
        return (
            <AgentListContainer
                email={email}
                onNewAgent={() => setView({name: 'new'})}
                onSelectAgent={(agentId) => setView({name: 'detail', agentId})}
                onOpenSettings={() => setView({name: 'settings'})}
                onNotConfigured={handleNotConfigured}
            />
        );
    }
};

export default Panel;
