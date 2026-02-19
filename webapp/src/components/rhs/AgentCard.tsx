import React from 'react';
import {useHistory} from 'react-router-dom';

import type {Agent} from '../../types';
import ExternalLink from '../common/ExternalLink';
import PhaseBadge, {getDisplayPhase} from '../common/PhaseBadge';
import StatusBadge from '../common/StatusBadge';

interface Props {
    agent: Agent;
    onClick: () => void;
    onArchive?: (e: React.MouseEvent) => void;
    onUnarchive?: (e: React.MouseEvent) => void;
    archiveLoading?: boolean;
    unarchiveLoading?: boolean;
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

const AgentCard: React.FC<Props> = ({agent, onClick, onArchive, onUnarchive, archiveLoading, unarchiveLoading}) => {
    const history = useHistory();
    const elapsed = getElapsedTime(agent.created_at);
    const isAborted = agent.status === 'STOPPED' || agent.status === 'FAILED';
    const displayPhase = getDisplayPhase(
        agent.workflow_phase,
        agent.review_loop_phase,
        isAborted,
    );
    const repoShort = agent.repository.split('/').slice(-2).join('/');
    const displayText = agent.description || agent.prompt || '';
    const promptPreview = displayText.length > 80 ?
        displayText.substring(0, 80) + '...' :
        displayText;

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
                {displayPhase && (
                    <>
                        <PhaseBadge phase={displayPhase}/>
                        {agent.review_loop_iteration && agent.review_loop_iteration > 1 && (
                            <span className='cursor-agent-card-badge'>
                                {`iter ${agent.review_loop_iteration}`}
                            </span>
                        )}
                    </>
                )}
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
            <div className='cursor-agent-card-links'>
                {agent.root_post_id && (
                    <a
                        className='cursor-agent-card-link'
                        href={`/${window.location.pathname.split('/')[1]}/pl/${agent.root_post_id}`}
                        onClick={(e) => {
                            e.preventDefault();
                            e.stopPropagation();
                            const teamName = window.location.pathname.split('/')[1];
                            history.push(`/${teamName}/pl/${agent.root_post_id}`);
                        }}
                    >
                        {'View Thread'}
                    </a>
                )}
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
                {onArchive && (
                    <button
                        className='cursor-agent-card-archive-btn'
                        onClick={onArchive}
                        title='Archive agent'
                        disabled={archiveLoading}
                    >
                        {archiveLoading ? (
                            <span className='cursor-agent-card-archive-spinner'/>
                        ) : (
                            'Archive'
                        )}
                    </button>
                )}
                {onUnarchive && (
                    <button
                        className='cursor-agent-card-archive-btn'
                        onClick={onUnarchive}
                        title='Unarchive agent'
                        disabled={unarchiveLoading}
                    >
                        {unarchiveLoading ? (
                            <span className='cursor-agent-card-archive-spinner'/>
                        ) : (
                            'Unarchive'
                        )}
                    </button>
                )}
            </div>
        </div>
    );
};

export default AgentCard;
