import React, {useState, useEffect} from 'react';
import {useDispatch, useSelector} from 'react-redux';
import {useHistory} from 'react-router-dom';

import type {GlobalState} from '@mattermost/types/store';

import {addFollowup, cancelAgent, fetchAgent, fetchReviewLoop, fetchWorkflow} from '../../actions';
import {getReviewLoopForAgent, getWorkflowForAgent} from '../../selectors';
import type {Agent, ReviewLoopPhase} from '../../types';
import ExternalLink from '../common/ExternalLink';
import PhaseBadge, {getDisplayPhase} from '../common/PhaseBadge';
import PhaseProgress from '../common/PhaseProgress';
import StatusBadge from '../common/StatusBadge';

interface Props {
    agent: Agent;
    onBack: () => void;
}

function getStatusBarClass(status: string, workflowPhase?: string, reviewLoopPhase?: string): string {
    // Review loop phases take priority for status bar color.
    if (reviewLoopPhase) {
        switch (reviewLoopPhase) {
        case 'requesting_review':
        case 'awaiting_review':
        case 'cursor_fixing':
            return 'cursor-agent-detail-status-bar--blue';
        case 'approved':
        case 'complete':
            return 'cursor-agent-detail-status-bar--green';
        case 'human_review':
            return 'cursor-agent-detail-status-bar--yellow';
        case 'max_iterations':
            return 'cursor-agent-detail-status-bar--grey';
        case 'failed':
            return 'cursor-agent-detail-status-bar--red';
        default:
            break;
        }
    }

    // If in a workflow, use phase-based coloring
    if (workflowPhase) {
        switch (workflowPhase) {
        case 'context_review':
        case 'plan_review':
            return 'cursor-agent-detail-status-bar--yellow';
        case 'planning':
        case 'implementing':
            return 'cursor-agent-detail-status-bar--blue';
        case 'rejected':
            return 'cursor-agent-detail-status-bar--red';
        case 'complete':
            return 'cursor-agent-detail-status-bar--green';
        default:
            break;
        }
    }

    // Fall back to agent status-based coloring
    switch (status) {
    case 'CREATING':
    case 'RUNNING':
        return 'cursor-agent-detail-status-bar--blue';
    case 'FINISHED':
        return 'cursor-agent-detail-status-bar--green';
    case 'FAILED':
        return 'cursor-agent-detail-status-bar--red';
    case 'STOPPED':
        return 'cursor-agent-detail-status-bar--yellow';
    default:
        return 'cursor-agent-detail-status-bar--grey';
    }
}

function getElapsedTime(timestamp: number): string {
    const diffMs = Date.now() - timestamp;
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

function getReviewLoopPhaseLabel(phase: string): string {
    switch (phase) {
    case 'requesting_review':
        return 'Review requested';
    case 'awaiting_review':
        return 'Awaiting AI review';
    case 'cursor_fixing':
        return 'Cursor fixing feedback';
    case 'approved':
        return 'AI approved';
    case 'human_review':
        return 'Human review';
    case 'complete':
        return 'Review complete';
    case 'max_iterations':
        return 'Max iterations reached';
    case 'failed':
        return 'Review failed';
    default:
        return phase;
    }
}

const ReviewLoopWhosUp: React.FC<{phase: ReviewLoopPhase}> = ({phase}) => {
    let label: string;
    let className: string;

    switch (phase) {
    case 'requesting_review':
        label = 'Requesting reviewers...';
        className = 'cursor-review-loop-whosup--active';
        break;
    case 'awaiting_review':
        label = 'CodeRabbit is reviewing';
        className = 'cursor-review-loop-whosup--active';
        break;
    case 'cursor_fixing':
        label = 'Cursor is fixing feedback';
        className = 'cursor-review-loop-whosup--active';
        break;
    case 'approved':
        label = 'Approved by CodeRabbit';
        className = 'cursor-review-loop-whosup--approved';
        break;
    case 'human_review':
        label = 'Waiting for human reviewer';
        className = 'cursor-review-loop-whosup--waiting';
        break;
    case 'complete':
        label = 'Review complete';
        className = 'cursor-review-loop-whosup--complete';
        break;
    case 'max_iterations':
        label = 'Needs manual review';
        className = 'cursor-review-loop-whosup--warning';
        break;
    case 'failed':
        label = 'Review failed';
        className = 'cursor-review-loop-whosup--failed';
        break;
    default:
        label = phase;
        className = '';
    }

    return <span className={`cursor-review-loop-whosup-label ${className}`}>{label}</span>;
};

const AgentDetail: React.FC<Props> = ({agent, onBack}) => {
    const dispatch = useDispatch();
    const history = useHistory();
    const [followupText, setFollowupText] = useState('');
    const isActive = agent.status === 'RUNNING' || agent.status === 'CREATING';
    const isAborted = agent.status === 'STOPPED' || agent.status === 'FAILED';
    const workflow = useSelector((state: GlobalState) => getWorkflowForAgent(state, agent.id));
    const reviewLoop = useSelector((state: GlobalState) => getReviewLoopForAgent(state, agent.id));
    const displayPhase = getDisplayPhase(workflow?.phase, reviewLoop?.phase, isAborted);

    // Always fetch latest agent data when opening detail view (Bug 3: dashboard may show stale state
    // after thread updates during a run).
    useEffect(() => {
        dispatch(fetchAgent(agent.id) as any);
    }, [agent.id, dispatch]);

    // Always fetch full workflow when agent has workflow_id (refresh on open to keep details current).
    useEffect(() => {
        if (agent.workflow_id) {
            dispatch(fetchWorkflow(agent.workflow_id) as any);
        }
    }, [agent.workflow_id, dispatch]);

    // Always fetch full review loop when agent has review_loop_id (refresh on open to keep details current).
    useEffect(() => {
        if (agent.review_loop_id) {
            dispatch(fetchReviewLoop(agent.review_loop_id) as any);
        }
    }, [agent.review_loop_id, dispatch]);

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
            <div className={`cursor-agent-detail-status-bar ${getStatusBarClass(agent.status, workflow?.phase, reviewLoop?.phase)}`}/>
            <div className='cursor-agent-detail-content'>
                <div className='cursor-agent-detail-back'>
                    <button
                        className='btn btn-link'
                        onClick={onBack}
                    >
                        {'< Back to list'}
                    </button>
                </div>

                {workflow && !(isAborted && workflow.phase !== 'rejected' && workflow.phase !== 'complete') && (
                    <PhaseProgress
                        phase={workflow.phase}
                        planIterationCount={workflow.plan_iteration_count}
                        skipContextReview={workflow.skip_context_review}
                        skipPlanLoop={workflow.skip_plan_loop}
                        reviewLoopPhase={reviewLoop?.phase}
                        reviewLoopIteration={reviewLoop?.iteration}
                    />
                )}

                <div className='cursor-agent-detail-header'>
                    <StatusBadge status={agent.status}/>
                    <span className='cursor-agent-detail-status-text'>{agent.status}</span>
                    {displayPhase && <PhaseBadge phase={displayPhase}/>}
                </div>

                <div className='cursor-agent-detail-section'>
                    <div className='cursor-agent-detail-label'>{'Repository'}</div>
                    <div className='cursor-agent-detail-value'>{agent.repository}</div>
                </div>

                {agent.branch && (
                    <div className='cursor-agent-detail-section'>
                        <div className='cursor-agent-detail-label'>{'Base Branch'}</div>
                        <div className='cursor-agent-detail-value'>{agent.branch}</div>
                    </div>
                )}

                {agent.target_branch && (
                    <div className='cursor-agent-detail-section'>
                        <div className='cursor-agent-detail-label'>{'Created Branch'}</div>
                        <div className='cursor-agent-detail-value'>{agent.target_branch}</div>
                    </div>
                )}

                {agent.model && (
                    <div className='cursor-agent-detail-section'>
                        <div className='cursor-agent-detail-label'>{'Model'}</div>
                        <div className='cursor-agent-detail-value'>{agent.model}</div>
                    </div>
                )}

                {(agent.description || agent.prompt) && (
                    <div className='cursor-agent-detail-section'>
                        <div className='cursor-agent-detail-label'>{'Description'}</div>
                        <div className='cursor-agent-detail-value'>{agent.description || agent.prompt}</div>
                    </div>
                )}

                {workflow?.enriched_context && (
                    <div className='cursor-agent-detail-section'>
                        <div className='cursor-agent-detail-label'>{'Enriched Context'}</div>
                        <div className='cursor-agent-detail-context'>{workflow.enriched_context}</div>
                    </div>
                )}

                {workflow?.retrieved_plan && (
                    <div className='cursor-agent-detail-section'>
                        <div className='cursor-agent-detail-label'>
                            {workflow.plan_iteration_count > 1 ?
                                `Implementation Plan (v${workflow.plan_iteration_count})` :
                                'Implementation Plan'
                            }
                        </div>
                        <div className='cursor-agent-detail-plan'>{workflow.retrieved_plan}</div>
                    </div>
                )}

                {agent.summary && (
                    <div className='cursor-agent-detail-section'>
                        <div className='cursor-agent-detail-label'>{'Summary'}</div>
                        <div className='cursor-agent-detail-value'>{agent.summary}</div>
                    </div>
                )}

                {reviewLoop && (
                    <div className='cursor-agent-detail-section'>
                        <div className='cursor-agent-detail-label'>{'AI Review'}</div>
                        <div className='cursor-review-loop-section'>
                            <div className='cursor-review-loop-whosup'>
                                <ReviewLoopWhosUp phase={reviewLoop.phase}/>
                            </div>
                            {reviewLoop.iteration > 0 && (
                                <div className='cursor-review-loop-iteration'>
                                    {`Iteration ${reviewLoop.iteration}`}
                                </div>
                            )}
                            {reviewLoop.pr_url && (
                                <div className='cursor-review-loop-pr'>
                                    <ExternalLink
                                        href={reviewLoop.pr_url}
                                        className='cursor-agent-card-link'
                                    >
                                        {'View PR'}
                                    </ExternalLink>
                                </div>
                            )}
                            {reviewLoop.history && reviewLoop.history.length > 0 && (
                                <div className='cursor-review-loop-timeline'>
                                    {reviewLoop.history.map((event, index) => (
                                        <div
                                            key={index}
                                            className='cursor-review-loop-timeline-item'
                                        >
                                            <div className='cursor-review-loop-timeline-dot'/>
                                            <div className='cursor-review-loop-timeline-content'>
                                                <span className='cursor-review-loop-timeline-phase'>
                                                    {getReviewLoopPhaseLabel(event.phase)}
                                                </span>
                                                {event.detail && (
                                                    <span className='cursor-review-loop-timeline-detail'>
                                                        {` -- ${event.detail}`}
                                                    </span>
                                                )}
                                                <span className='cursor-review-loop-timeline-time'>
                                                    {getElapsedTime(event.timestamp)}
                                                </span>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>
                    </div>
                )}
            </div>

            <div className='cursor-agent-detail-footer'>
                <div className='cursor-agent-detail-links'>
                    {agent.root_post_id && (
                        <a
                            href={`/${window.location.pathname.split('/')[1]}/pl/${agent.root_post_id}`}
                            className='btn btn-tertiary'
                            onClick={(e) => {
                                e.preventDefault();
                                const teamName = window.location.pathname.split('/')[1];
                                history.push(`/${teamName}/pl/${agent.root_post_id}`);
                            }}
                        >
                            {'View Thread'}
                        </a>
                    )}
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

                {isActive && (
                    <>
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

                        <div className='cursor-agent-detail-cancel'>
                            <button
                                className='btn btn-danger'
                                onClick={handleCancel}
                            >
                                {'Cancel Agent'}
                            </button>
                        </div>
                    </>
                )}
            </div>
        </div>
    );
};

export default AgentDetail;
