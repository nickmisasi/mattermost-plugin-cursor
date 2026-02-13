import React from 'react';

import AgentCard from './AgentCard';

import type {Agent} from '../../types';

interface Props {
    agents: Agent[];
    isLoading: boolean;
    onSelectAgent: (agentId: string) => void;
}

const AgentList: React.FC<Props> = ({agents, isLoading, onSelectAgent}) => {
    const sorted = [...agents].sort((a, b) => b.created_at - a.created_at);

    if (isLoading && agents.length === 0) {
        return <div className='cursor-rhs-empty'>{'Loading agents...'}</div>;
    }

    if (agents.length === 0) {
        return (
            <div className='cursor-rhs-empty'>
                <p>{'No agents yet.'}</p>
                <p className='cursor-rhs-empty-hint'>
                    {'Mention '}<strong>{'@cursor'}</strong>{' in any channel to launch a background agent.'}
                </p>
            </div>
        );
    }

    return (
        <div className='cursor-agent-list'>
            {sorted.map((agent) => (
                <AgentCard
                    key={agent.id}
                    agent={agent}
                    onClick={() => onSelectAgent(agent.id)}
                />
            ))}
        </div>
    );
};

export default AgentList;
