import React, {useState, useCallback} from 'react';
import {useDispatch} from 'react-redux';

import AgentCard from './AgentCard';

import {archiveAgent, unarchiveAgent, openSettings} from '../../actions';
import type {Agent} from '../../types';
import ConfirmModal from '../common/ConfirmModal';

interface Props {
    agents: Agent[];
    isLoading: boolean;
    onSelectAgent: (agentId: string) => void;
    showArchived: boolean;
    onTabChange: (archived: boolean) => void;
}

const AgentCardWithActions: React.FC<{
    agent: Agent;
    onClick: () => void;
    showArchived: boolean;
    loadingAgentId: string | null;
    onArchiveClick: (e: React.MouseEvent, agentId: string) => void;
    onUnarchiveClick: (e: React.MouseEvent, agentId: string) => void;
}> = ({agent, onClick, showArchived, loadingAgentId, onArchiveClick, onUnarchiveClick}) => {
    const archiveLoading = loadingAgentId === agent.id && !showArchived;
    const unarchiveLoading = loadingAgentId === agent.id && showArchived;

    return (
        <AgentCard
            agent={agent}
            onClick={onClick}
            onArchive={showArchived ? undefined : (e) => onArchiveClick(e, agent.id)}
            onUnarchive={showArchived ? (e) => onUnarchiveClick(e, agent.id) : undefined}
            archiveLoading={archiveLoading}
            unarchiveLoading={unarchiveLoading}
        />
    );
};

const SettingsCog: React.FC = () => {
    const dispatch = useDispatch();

    return (
        <button
            className='btn btn-link cursor-settings-cog'
            onClick={() => dispatch(openSettings() as any)}
            title='Cursor Settings'
            aria-label='Cursor Settings'
        >
            <svg
                width='18'
                height='18'
                viewBox='0 0 24 24'
                fill='none'
                stroke='currentColor'
                strokeWidth='2'
                strokeLinecap='round'
                strokeLinejoin='round'
                xmlns='http://www.w3.org/2000/svg'
            >
                <circle
                    cx='12'
                    cy='12'
                    r='3'
                />
                <path d='M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z'/>
            </svg>
        </button>
    );
};

const AgentList: React.FC<Props> = ({agents, isLoading, onSelectAgent, showArchived, onTabChange}) => {
    const dispatch = useDispatch();
    const [confirmModal, setConfirmModal] = useState<{agentId: string; action: 'archive' | 'unarchive'} | null>(null);
    const [loadingAgentId, setLoadingAgentId] = useState<string | null>(null);

    const handleArchiveClick = useCallback((e: React.MouseEvent, agentId: string) => {
        e.stopPropagation();
        setConfirmModal({agentId, action: 'archive'});
    }, []);

    const handleUnarchiveClick = useCallback((e: React.MouseEvent, agentId: string) => {
        e.stopPropagation();
        setConfirmModal({agentId, action: 'unarchive'});
    }, []);

    const handleConfirmModalConfirm = useCallback(() => {
        if (!confirmModal) {
            return;
        }
        const {agentId, action} = confirmModal;
        setConfirmModal(null);
        setLoadingAgentId(agentId);
        const thunk = action === 'archive' ? archiveAgent(agentId) : unarchiveAgent(agentId);
        (dispatch(thunk as any) as Promise<void>).finally(() => {
            setLoadingAgentId(null);
        });
    }, [confirmModal, dispatch]);

    const handleConfirmModalCancel = useCallback(() => {
        setConfirmModal(null);
    }, []);

    const sorted = [...agents].sort((a, b) => b.created_at - a.created_at);

    return (
        <div className='cursor-agent-list'>
            <div className='cursor-tab-bar'>
                <div className='cursor-tab-bar-tabs'>
                    <button
                        className={`cursor-tab${showArchived ? '' : ' cursor-tab--active'}`}
                        onClick={() => onTabChange(false)}
                    >
                        {'Active'}
                    </button>
                    <button
                        className={`cursor-tab${showArchived ? ' cursor-tab--active' : ''}`}
                        onClick={() => onTabChange(true)}
                    >
                        {'Archived'}
                    </button>
                </div>
                <SettingsCog/>
            </div>

            {isLoading && agents.length === 0 && (
                <div className='cursor-rhs-empty'>{'Loading agents...'}</div>
            )}

            {!isLoading && agents.length === 0 && (
                <div className='cursor-rhs-empty'>
                    {showArchived ? (
                        <p>{'No archived agents.'}</p>
                    ) : (
                        <>
                            <p>{'No agents yet.'}</p>
                            <p className='cursor-rhs-empty-hint'>
                                {'Mention '}<strong>{'@cursor'}</strong>{' in any channel to launch a background agent.'}
                            </p>
                        </>
                    )}
                </div>
            )}

            {sorted.map((agent) => (
                <AgentCardWithActions
                    key={agent.id}
                    agent={agent}
                    onClick={() => onSelectAgent(agent.id)}
                    showArchived={showArchived}
                    loadingAgentId={loadingAgentId}
                    onArchiveClick={handleArchiveClick}
                    onUnarchiveClick={handleUnarchiveClick}
                />
            ))}

            <ConfirmModal
                show={confirmModal !== null}
                title={
                    confirmModal?.action === 'archive' ?
                        'Archive Agent' :
                        'Unarchive Agent'
                }
                message={
                    confirmModal?.action === 'archive' ?
                        'Are you sure you want to archive this agent? It will be moved to the Archived tab.' :
                        'Are you sure you want to unarchive this agent? It will be moved back to the Active tab.'
                }
                confirmText={
                    confirmModal?.action === 'archive' ?
                        'Archive' :
                        'Unarchive'
                }
                confirmClassName={
                    confirmModal?.action === 'archive' ?
                        'btn-danger' :
                        'btn-primary'
                }
                onConfirm={handleConfirmModalConfirm}
                onCancel={handleConfirmModalCancel}
            />
        </div>
    );
};

export default AgentList;
