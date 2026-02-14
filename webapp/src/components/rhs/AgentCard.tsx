import React from 'react';

import type {Agent} from '../../types';
import ExternalLink from '../common/ExternalLink';
import StatusBadge from '../common/StatusBadge';

interface Props {
    agent: Agent;
    onClick: () => void;
}

function getElapsedTime(createdAt: number): string {
    const diffMs = Date.now() - createdAt;
    const minutes = Math.floor(diffMs / 60000);
    if (minutes < 1) {
        return 'just now';
    }
    if (minutes < 60) {
        return `${minutes}m ago`;
    }
    const hours = Math.floor(minutes / 60);
    if (hours < 24) {
        return `${hours}h ago`;
    }
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
}

const AgentCard: React.FC<Props> = ({agent, onClick}) => {
    const elapsed = getElapsedTime(agent.created_at);
    const repoShort = agent.repository.split('/').slice(-2).join('/');
    const promptPreview = agent.prompt && agent.prompt.length > 80 ?
        agent.prompt.substring(0, 80) + '...' :
        (agent.prompt || '');

    return (
        <div
            className='cursor-agent-card'
            onClick={onClick}
            role='button'
            tabIndex={0}
            onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    onClick();
                }
            }}
        >
            <div className='cursor-agent-card-header'>
                <StatusBadge status={agent.status}/>
                <span className='cursor-agent-card-repo'>{repoShort}</span>
                <span className='cursor-agent-card-time'>{elapsed}</span>
            </div>
            {(agent.branch || agent.model) && (
                <div className='cursor-agent-card-meta'>
                    {agent.branch && (
                        <span className='cursor-agent-card-badge'>{agent.branch}</span>
                    )}
                    {agent.model && (
                        <span className='cursor-agent-card-badge'>{agent.model}</span>
                    )}
                </div>
            )}
            {promptPreview && (
                <div className='cursor-agent-card-prompt'>{promptPreview}</div>
            )}
            {(agent.pr_url || agent.cursor_url) && (
                <div className='cursor-agent-card-links'>
                    {agent.pr_url && (
                        <ExternalLink
                            className='cursor-agent-card-link'
                            href={agent.pr_url}
                            onClick={(e) => e.stopPropagation()}
                        >
                            {'View PR'}
                        </ExternalLink>
                    )}
                    {agent.cursor_url && (
                        <ExternalLink
                            className='cursor-agent-card-link'
                            href={agent.cursor_url}
                            onClick={(e) => e.stopPropagation()}
                        >
                            {'Open in Cursor'}
                        </ExternalLink>
                    )}
                </div>
            )}
        </div>
    );
};

export default AgentCard;
