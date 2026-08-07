import React, {useCallback, useState} from 'react';

import AgentActionsMenu from './AgentActionsMenu';
import Composer from './Composer';
import ConversationMessage from './ConversationMessage';
import StatusBadge from './StatusBadge';

import Client, {errorMessage, isNotConfiguredError} from '../client';
import {hasActiveRun, resolveRunStatus, useAgentDetail} from '../hooks/useAgentDetail';
import {useRunStream} from '../hooks/useRunStream';
import {formatDateTime, formatDuration, relativeTime} from '../utils/time';

interface Props {
    agentId: string;
    onBack: () => void;
    onNotConfigured: () => void;
    onDeleted: () => void;
}

const AgentDetailView = ({agentId, onBack, onNotConfigured, onDeleted}: Props) => {
    const {agent, run, messages, loading, error, reload} = useAgentDetail(agentId, onNotConfigured);
    const [actionError, setActionError] = useState('');
    const [busy, setBusy] = useState(false);

    const runStatus = resolveRunStatus(agent, run);
    const active = hasActiveRun(agent, run);
    const runId = run?.id || agent?.latestRunId || '';

    const onStreamFinished = useCallback(() => reload(), [reload]);
    const stream = useRunStream(agentId, runId, active, onStreamFinished);

    const perform = useCallback(async (action: () => Promise<unknown>, failure: string, afterSuccess?: () => void) => {
        setBusy(true);
        setActionError('');
        try {
            await action();
            if (afterSuccess) {
                afterSuccess();
            } else {
                reload();
            }
        } catch (err) {
            if (isNotConfiguredError(err)) {
                onNotConfigured();
            } else {
                setActionError(errorMessage(err, failure));
            }
        } finally {
            setBusy(false);
        }
    }, [onNotConfigured, reload]);

    const cancelRun = useCallback(() => {
        perform(() => Client.cancelRun(agentId, runId), 'Could not cancel the run.');
    }, [agentId, perform, runId]);

    const toggleArchive = useCallback(() => {
        const archived = agent?.archived ?? false;
        perform(
            () => (archived ? Client.unarchiveAgent(agentId) : Client.archiveAgent(agentId)),
            'Could not update the agent.',
        );
    }, [agent?.archived, agentId, perform]);

    const remove = useCallback(() => {
        perform(() => Client.deleteAgent(agentId), 'Could not delete the agent.', onDeleted);
    }, [agentId, onDeleted, perform]);

    if (loading && !agent) {
        return (
            <div className='cursor-panel'>
                <p className='cursor-placeholder'>{'Loading agent…'}</p>
            </div>
        );
    }

    if (!agent) {
        return (
            <div className='cursor-panel'>
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
                </header>
                <p
                    className='cursor-error'
                    role='alert'
                >
                    {error || 'This agent could not be found.'}
                </p>
            </div>
        );
    }

    const branch = agent.branch || run?.branch || agent.startingRef;
    const prUrl = agent.prUrl || run?.prUrl || '';
    const duration = formatDuration(run?.durationMs);

    return (
        <div className='cursor-panel'>
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
                <span
                    className='cursor-header__title'
                    title={agent.name}
                >
                    {agent.name}
                </span>
                <AgentActionsMenu
                    canCancel={active && Boolean(runId)}
                    archived={agent.archived}
                    busy={busy}
                    onCancelRun={cancelRun}
                    onToggleArchive={toggleArchive}
                    onDelete={remove}
                />
            </header>

            <div className='cursor-detail__summary'>
                <StatusBadge
                    status={runStatus}
                    archived={agent.archived}
                />
                <span
                    className='cursor-detail__time'
                    title={formatDateTime(agent.activityAt)}
                >
                    {duration ? `${duration} · ` : ''}
                    {relativeTime(agent.activityAt)}
                </span>
            </div>

            <div className='cursor-detail__facts'>
                <span title={agent.repositoryUrl}>
                    <i className='icon icon-folder-outline'/>
                    {agent.repository}
                </span>
                {branch ? (
                    <span title={branch}>
                        <i className='icon icon-source-branch'/>
                        {branch}
                    </span>
                ) : null}
                {prUrl ? (
                    <a
                        href={prUrl}
                        target='_blank'
                        rel='noopener noreferrer'
                    >
                        <i className='icon icon-source-pull'/>
                        {'Pull request'}
                    </a>
                ) : null}
                {agent.webUrl ? (
                    <a
                        href={agent.webUrl}
                        target='_blank'
                        rel='noopener noreferrer'
                    >
                        <i className='icon icon-open-in-new'/>
                        {'Open in Cursor'}
                    </a>
                ) : null}
            </div>

            {actionError || error ? (
                <p
                    className='cursor-error cursor-error--banner'
                    role='alert'
                >
                    {actionError || error}
                </p>
            ) : null}

            <div className='cursor-scroll cursor-conversation'>
                {messages.length === 0 && !stream.text ? (
                    <p className='cursor-placeholder'>{'No conversation yet.'}</p>
                ) : null}
                {messages.map((message) => (
                    <ConversationMessage
                        key={message.id}
                        message={message}
                    />
                ))}
                {stream.streaming || stream.text ? (
                    <article className='cursor-message cursor-message--assistant cursor-message--live'>
                        <span className='cursor-message__author'>{'Cursor'}</span>
                        {stream.text ? <p className='cursor-message__text'>{stream.text}</p> : null}
                        <p className='cursor-activity'>{stream.activity || 'Working…'}</p>
                    </article>
                ) : null}
                {stream.error ? (
                    <p
                        className='cursor-error'
                        role='alert'
                    >
                        {stream.error}
                    </p>
                ) : null}
            </div>

            {agent.archived ? null : (
                <Composer
                    agentId={agentId}
                    onSent={reload}
                    onNotConfigured={onNotConfigured}
                />
            )}
        </div>
    );
};

export default AgentDetailView;
