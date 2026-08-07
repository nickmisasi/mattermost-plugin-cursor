import React, {useCallback, useState} from 'react';

import AutoTextarea from './AutoTextarea';

import Client, {ClientError, errorMessage, isNotConfiguredError} from '../client';

interface Props {
    agentId: string;
    onSent: () => void;
    onNotConfigured: () => void;
}

const Composer = ({agentId, onSent, onNotConfigured}: Props) => {
    const [value, setValue] = useState('');
    const [sending, setSending] = useState(false);
    const [error, setError] = useState('');

    const send = useCallback(async () => {
        const prompt = value.trim();
        if (!prompt || sending) {
            return;
        }

        setSending(true);
        setError('');
        try {
            await Client.followup(agentId, prompt);
            setValue('');
            onSent();
        } catch (err) {
            if (isNotConfiguredError(err)) {
                onNotConfigured();
                return;
            }
            if (err instanceof ClientError && err.status === 409) {
                setError('Agent is still running. Wait for the current run to finish before sending a follow-up.');
                return;
            }
            setError(errorMessage(err, 'Could not send the follow-up.'));
        } finally {
            setSending(false);
        }
    }, [agentId, onNotConfigured, onSent, sending, value]);

    return (
        <div className='cursor-composer'>
            {error ? (
                <p
                    className='cursor-error'
                    role='alert'
                >
                    {error}
                </p>
            ) : null}
            <div className='cursor-composer__row'>
                <AutoTextarea
                    className='cursor-textarea cursor-composer__input'
                    placeholder='Send a follow-up…'
                    minRows={1}
                    maxRows={8}
                    value={value}
                    disabled={sending}
                    onChange={setValue}
                    onSubmit={() => send()}
                />
                <button
                    type='button'
                    className='btn btn-primary btn-icon cursor-icon-button'
                    onClick={() => send()}
                    disabled={sending || !value.trim()}
                    aria-label='Send follow-up'
                    title='Send follow-up'
                >
                    <i className='icon icon-send'/>
                </button>
            </div>
        </div>
    );
};

export default Composer;
