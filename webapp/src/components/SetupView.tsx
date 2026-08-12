import React, {useState} from 'react';

import Client, {errorMessage} from '../client';

const DASHBOARD_URL = 'https://cursor.com/dashboard';

interface Props {
    mode: 'connect' | 'manage';
    email?: string;
    onConnected: (email: string) => void;
    onDisconnected?: () => void;
    onBack?: () => void;
}

const SetupView = ({mode, email = '', onConnected, onDisconnected, onBack}: Props) => {
    const [apiKey, setApiKey] = useState('');
    const [saving, setSaving] = useState(false);
    const [disconnecting, setDisconnecting] = useState(false);
    const [error, setError] = useState('');

    const isManage = mode === 'manage';

    const submit = async (event: React.FormEvent) => {
        event.preventDefault();
        if (!apiKey.trim() || saving) {
            return;
        }

        setSaving(true);
        setError('');
        try {
            const status = await Client.setKey(apiKey.trim());
            setApiKey('');
            onConnected(status.email);
        } catch (err) {
            setError(errorMessage(err, 'That API key could not be verified.'));
        } finally {
            setSaving(false);
        }
    };

    const disconnect = async () => {
        setDisconnecting(true);
        setError('');
        try {
            await Client.deleteKey();
            onDisconnected?.();
        } catch (err) {
            setError(errorMessage(err, 'Could not disconnect your Cursor account.'));
        } finally {
            setDisconnecting(false);
        }
    };

    return (
        <div className='cursor-panel'>
            {isManage ? (
                <header className='cursor-header'>
                    <button
                        type='button'
                        className='btn btn-tertiary btn-icon cursor-icon-button'
                        onClick={onBack}
                        aria-label='Back to agents'
                        title='Back to agents'
                    >
                        <i className='icon icon-arrow-left'/>
                    </button>
                    <span className='cursor-header__title'>{'Cursor connection'}</span>
                </header>
            ) : null}

            <div className='cursor-scroll cursor-setup'>
                <h2 className='cursor-setup__title'>
                    {isManage ? 'Manage your Cursor connection' : 'Connect your Cursor account'}
                </h2>
                <p className='cursor-setup__body'>
                    {isManage ?
                        'Replace your API key at any time, or disconnect to remove it from Mattermost.' :
                        'Connect your Cursor account to launch and manage Cloud Agents without leaving Mattermost.'}
                </p>

                {isManage && email ? (
                    <p className='cursor-setup__connected'>
                        <i className='icon icon-check-circle-outline'/>
                        {`Connected as ${email}`}
                    </p>
                ) : null}

                <form
                    className='cursor-setup__form'
                    onSubmit={submit}
                >
                    <label
                        className='cursor-label'
                        htmlFor='cursor-api-key'
                    >
                        {isManage ? 'New API key' : 'Cursor API key'}
                    </label>
                    <input
                        id='cursor-api-key'
                        className='cursor-input'
                        type='password'
                        autoComplete='off'
                        placeholder='key_...'
                        value={apiKey}
                        onChange={(event) => setApiKey(event.target.value)}
                    />
                    <p className='cursor-hint'>
                        {'Create a key at '}
                        <a
                            href={DASHBOARD_URL}
                            target='_blank'
                            rel='noopener noreferrer'
                        >
                            {DASHBOARD_URL}
                        </a>
                        {' under Integrations. Your key is stored per user and is never shared.'}
                    </p>

                    {error ? (
                        <p
                            className='cursor-error'
                            role='alert'
                        >
                            {error}
                        </p>
                    ) : null}

                    <div className='cursor-setup__actions'>
                        <button
                            type='submit'
                            className='btn btn-primary'
                            disabled={saving || !apiKey.trim()}
                        >
                            {saving ? 'Connecting…' : 'Connect'}
                        </button>
                        {isManage ? (
                            <button
                                type='button'
                                className='btn btn-danger'
                                onClick={disconnect}
                                disabled={disconnecting}
                            >
                                {disconnecting ? 'Disconnecting…' : 'Disconnect'}
                            </button>
                        ) : null}
                    </div>
                </form>
            </div>
        </div>
    );
};

export default SetupView;
