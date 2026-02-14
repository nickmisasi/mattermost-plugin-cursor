import React from 'react';
import {useDispatch} from 'react-redux';

import AgentCard from './AgentCard';

import {archiveAgent, unarchiveAgent, openSettings} from '../../actions';
import type {Agent} from '../../types';

interface Props {
    agents: Agent[];
    isLoading: boolean;
    onSelectAgent: (agentId: string) => void;
    showArchived: boolean;
    onTabChange: (archived: boolean) => void;
}

const AgentCardWithActions: React.FC<{agent: Agent; onClick: () => void; showArchived: boolean}> = ({agent, onClick, showArchived}) => {
    const dispatch = useDispatch();

    const handleArchive = (e: React.MouseEvent) => {
        e.stopPropagation();
        dispatch(archiveAgent(agent.id) as any);
    };

    const handleUnarchive = (e: React.MouseEvent) => {
        e.stopPropagation();
        dispatch(unarchiveAgent(agent.id) as any);
    };

    return (
        <AgentCard
            agent={agent}
            onClick={onClick}
            onArchive={showArchived ? undefined : handleArchive}
            onUnarchive={showArchived ? handleUnarchive : undefined}
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
                />
            ))}
        </div>
    );
};

export default AgentList;
