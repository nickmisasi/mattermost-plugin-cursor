import React, {useState} from 'react';

import AgentRow from './AgentRow';

import type {RepoGroup as RepoGroupModel} from '../types';
import {COLLAPSED_GROUP_SIZE} from '../utils/grouping';

interface Props {
    group: RepoGroupModel;
    onSelect: (agentId: string) => void;
}

const RepoGroup = ({group, onSelect}: Props) => {
    const [expanded, setExpanded] = useState(false);

    const hidden = group.agents.length - COLLAPSED_GROUP_SIZE;
    const visible = expanded ? group.agents : group.agents.slice(0, COLLAPSED_GROUP_SIZE);

    return (
        <section className='cursor-repo-group'>
            <h3
                className='cursor-repo-group__header'
                title={group.repositoryUrl || group.repository}
            >
                <i className='icon icon-folder-outline'/>
                <span className='cursor-repo-group__name'>{group.repository}</span>
            </h3>
            {visible.map((agent) => (
                <AgentRow
                    key={agent.id}
                    agent={agent}
                    onSelect={onSelect}
                />
            ))}
            {hidden > 0 && !expanded ? (
                <button
                    type='button'
                    className='cursor-more-row'
                    onClick={() => setExpanded(true)}
                >
                    {`More (${hidden})`}
                </button>
            ) : null}
        </section>
    );
};

export default RepoGroup;
