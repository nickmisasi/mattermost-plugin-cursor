import React, {useState} from 'react';
import {useDispatch} from 'react-redux';

import {addFollowup, cancelAgent} from '../../actions';
import type {Agent} from '../../types';
import ExternalLink from '../common/ExternalLink';
import StatusBadge from '../common/StatusBadge';

interface Props {
    agent: Agent;
    onBack: () => void;
}

function getStatusBarClass(status: string): string {
    switch (status) {
    case 'CREATING':
    case 'RUNNING':
        return 'cursor-agent-detail-status-bar--blue';
    case 'FINISHED':
        return 'cursor-agent-detail-status-bar--green';
    case 'FAILED':
        return 'cursor-agent-detail-status-bar--red';
    case 'STOPPED':
        return 'cursor-agent-detail-status-bar--grey';
    default:
        return 'cursor-agent-detail-status-bar--grey';
    }
}

const AgentDetail: React.FC<Props> = ({agent, onBack}) => {
    const dispatch = useDispatch();
    const [followupText, setFollowupText] = useState('');
    const isActive = agent.status === 'RUNNING' || agent.status === 'CREATING';

    const handleFollowup = () => {
        if (followupText.trim() && agent.status === 'RUNNING') {
            dispatch(addFollowup(agent.id, followupText.trim()) as any);
            setFollowupText('');
        }
    };

    const handleCancel = () => {
        if (window.confirm('Are you sure you want to cancel this agent?')) { // eslint-disable-line no-alert
            dispatch(cancelAgent(agent.id) as any);
        }
    };

    return (
        <div className='cursor-agent-detail'>
            <div className={`cursor-agent-detail-status-bar ${getStatusBarClass(agent.status)}`}/>
            <div className='cursor-agent-detail-back'>
                <button
                    className='btn btn-link'
                    onClick={onBack}
                >
                    {'< Back to list'}
                </button>
            </div>

            <div className='cursor-agent-detail-header'>
                <StatusBadge status={agent.status}/>
                <span className='cursor-agent-detail-status-text'>{agent.status}</span>
            </div>

            <div className='cursor-agent-detail-section'>
                <div className='cursor-agent-detail-label'>{'Repository'}</div>
                <div className='cursor-agent-detail-value'>{agent.repository}</div>
            </div>

            {agent.branch && (
                <div className='cursor-agent-detail-section'>
                    <div className='cursor-agent-detail-label'>{'Branch'}</div>
                    <div className='cursor-agent-detail-value'>{agent.branch}</div>
                </div>
            )}

            {agent.model && (
                <div className='cursor-agent-detail-section'>
                    <div className='cursor-agent-detail-label'>{'Model'}</div>
                    <div className='cursor-agent-detail-value'>{agent.model}</div>
                </div>
            )}

            {agent.prompt && (
                <div className='cursor-agent-detail-section'>
                    <div className='cursor-agent-detail-label'>{'Prompt'}</div>
                    <div className='cursor-agent-detail-value'>{agent.prompt}</div>
                </div>
            )}

            {agent.summary && (
                <div className='cursor-agent-detail-section'>
                    <div className='cursor-agent-detail-label'>{'Summary'}</div>
                    <div className='cursor-agent-detail-value'>{agent.summary}</div>
                </div>
            )}

            <div className='cursor-agent-detail-links'>
                {agent.cursor_url && (
                    <ExternalLink
                        href={agent.cursor_url}
                        className='btn btn-tertiary'
                    >
                        {'Open in Cursor'}
                    </ExternalLink>
                )}
                {agent.pr_url && (
                    <ExternalLink
                        href={agent.pr_url}
                        className='btn btn-primary'
                    >
                        {'View Pull Request'}
                    </ExternalLink>
                )}
            </div>

            {agent.status === 'RUNNING' && (
                <div className='cursor-agent-detail-followup'>
                    <div className='cursor-agent-detail-label'>{'Send Follow-up'}</div>
                    <textarea
                        className='cursor-textarea'
                        placeholder='Enter follow-up instructions...'
                        value={followupText}
                        onChange={(e) => setFollowupText(e.target.value)}
                        rows={3}
                    />
                    <button
                        className='btn btn-primary'
                        onClick={handleFollowup}
                        disabled={!followupText.trim()}
                    >
                        {'Send'}
                    </button>
                </div>
            )}

            {isActive && (
                <div className='cursor-agent-detail-cancel'>
                    <button
                        className='btn btn-danger'
                        onClick={handleCancel}
                    >
                        {'Cancel Agent'}
                    </button>
                </div>
            )}
        </div>
    );
};

export default AgentDetail;
