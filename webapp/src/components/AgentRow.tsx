import React from 'react';

import StatusDot from './StatusDot';

import type {Agent} from '../types';
import {formatDateTime, relativeTime} from '../utils/time';

interface Props {
    agent: Agent;
    onSelect: (agentId: string) => void;
}

const AgentRow = ({agent, onSelect}: Props) => {
    const subtitle = agent.branch || agent.startingRef;

    return (
        <button
            type='button'
            className='cursor-agent-row'
            onClick={() => onSelect(agent.id)}
        >
            <StatusDot
                status={agent.runStatus}
                archived={agent.archived}
            />
            <span className='cursor-agent-row__body'>
                <span className='cursor-agent-row__title'>{agent.name}</span>
                {subtitle ? (
                    <span className='cursor-agent-row__branch'>
                        <i className='icon icon-source-branch'/>
                        {subtitle}
                    </span>
                ) : null}
            </span>
            <span className='cursor-agent-row__meta'>
                {agent.prUrl ? (
                    <i
                        className='icon icon-source-pull'
                        title='Pull request opened'
                    />
                ) : null}
                {agent.envType === 'cloud' ? (
                    <i
                        className='icon icon-cloud-outline'
                        title='Runs in Cursor Cloud'
                    />
                ) : null}
                <span
                    className='cursor-agent-row__time'
                    title={formatDateTime(agent.activityAt)}
                >
                    {relativeTime(agent.activityAt)}
                </span>
            </span>
        </button>
    );
};

export default AgentRow;
