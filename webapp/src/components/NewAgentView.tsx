import React, {useCallback, useEffect, useRef, useState} from 'react';

import AutoTextarea from './AutoTextarea';

import Client, {errorMessage, isNotConfiguredError} from '../client';
import type {ModelOption, RepositoryOption} from '../types';
import {normalizeCreateAgentResponse, normalizeModels, normalizeRepositories} from '../utils/normalize';

interface Props {
    onCancel: () => void;
    onCreated: (agentId: string) => void;
    onNotConfigured: () => void;
}

const NewAgentView = ({onCancel, onCreated, onNotConfigured}: Props) => {
    const [prompt, setPrompt] = useState('');
    const [repository, setRepository] = useState('');
    const [ref, setRef] = useState('');
    const [model, setModel] = useState('');
    const [autoCreatePr, setAutoCreatePr] = useState(true);

    const [repositories, setRepositories] = useState<RepositoryOption[]>([]);
    const [models, setModels] = useState<ModelOption[]>([]);
    const [launching, setLaunching] = useState(false);
    const [error, setError] = useState('');

    const mounted = useRef(true);

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
        };
    }, []);

    useEffect(() => {
        // Both endpoints are cached and rate limited upstream; a failure just
        // means the user types the values by hand.
        Promise.all([
            Client.listRepositories().catch(() => null),
            Client.listModels().catch(() => null),
        ]).then(([repoPayload, modelPayload]) => {
            if (mounted.current) {
                setRepositories(normalizeRepositories(repoPayload));
                setModels(normalizeModels(modelPayload));
            }
        });
    }, []);

    const launch = useCallback(async (event: React.FormEvent) => {
        event.preventDefault();
        if (launching || !prompt.trim() || !repository.trim()) {
            return;
        }

        setLaunching(true);
        setError('');
        try {
            const payload = await Client.createAgent({
                prompt: prompt.trim(),
                repository: repository.trim(),
                ref: ref.trim() || undefined,
                model: model || undefined,
                autoCreatePr,
            });

            const {agent} = normalizeCreateAgentResponse(payload);
            if (agent) {
                onCreated(agent.id);
                return;
            }
            setError('The agent was created but Cursor did not return its ID. Refresh the list to find it.');
        } catch (err) {
            if (isNotConfiguredError(err)) {
                onNotConfigured();
                return;
            }
            setError(errorMessage(err, 'Could not launch the agent.'));
        } finally {
            if (mounted.current) {
                setLaunching(false);
            }
        }
    }, [autoCreatePr, launching, model, onCreated, onNotConfigured, prompt, ref, repository]);

    return (
        <div className='cursor-panel'>
            <header className='cursor-header'>
                <button
                    type='button'
                    className='btn btn-tertiary btn-icon cursor-icon-button'
                    onClick={onCancel}
                    aria-label='Back to agents'
                    title='Back to agents'
                >
                    <i className='icon icon-arrow-left'/>
                </button>
                <span className='cursor-header__title'>{'New Agent'}</span>
            </header>

            <form
                className='cursor-scroll cursor-form'
                onSubmit={launch}
            >
                <label
                    className='cursor-label'
                    htmlFor='cursor-prompt'
                >
                    {'Prompt'}
                </label>
                <AutoTextarea
                    id='cursor-prompt'
                    className='cursor-textarea'
                    autoFocus={true}
                    minRows={4}
                    placeholder='Describe the task for the agent…'
                    value={prompt}
                    onChange={setPrompt}
                />

                <label
                    className='cursor-label'
                    htmlFor='cursor-repository'
                >
                    {'Repository'}
                </label>
                <input
                    id='cursor-repository'
                    className='cursor-input'
                    list='cursor-repository-options'
                    placeholder='https://github.com/org/repo'
                    value={repository}
                    onChange={(event) => setRepository(event.target.value)}
                />
                <datalist id='cursor-repository-options'>
                    {repositories.map((option) => (
                        <option
                            key={option.url}
                            value={option.url}
                        >
                            {option.label}
                        </option>
                    ))}
                </datalist>

                <label
                    className='cursor-label'
                    htmlFor='cursor-ref'
                >
                    {'Starting branch or commit'}
                </label>
                <input
                    id='cursor-ref'
                    className='cursor-input'
                    placeholder='main'
                    value={ref}
                    onChange={(event) => setRef(event.target.value)}
                />

                <label
                    className='cursor-label'
                    htmlFor='cursor-model'
                >
                    {'Model'}
                </label>
                <select
                    id='cursor-model'
                    className='cursor-input'
                    value={model}
                    onChange={(event) => setModel(event.target.value)}
                >
                    <option value=''>{'Auto'}</option>
                    {models.map((option) => (
                        <option
                            key={option.id}
                            value={option.id}
                            title={option.description}
                        >
                            {option.displayName}
                        </option>
                    ))}
                </select>

                <label className='cursor-checkbox'>
                    <input
                        type='checkbox'
                        checked={autoCreatePr}
                        onChange={(event) => setAutoCreatePr(event.target.checked)}
                    />
                    {'Create PR automatically'}
                </label>

                {error ? (
                    <p
                        className='cursor-error'
                        role='alert'
                    >
                        {error}
                    </p>
                ) : null}

                <div className='cursor-form__actions'>
                    <button
                        type='submit'
                        className='btn btn-primary'
                        disabled={launching || !prompt.trim() || !repository.trim()}
                    >
                        {launching ? 'Launching…' : 'Launch agent'}
                    </button>
                    <button
                        type='button'
                        className='btn btn-tertiary'
                        onClick={onCancel}
                    >
                        {'Cancel'}
                    </button>
                </div>
            </form>
        </div>
    );
};

export default NewAgentView;
