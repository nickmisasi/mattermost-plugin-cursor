import React, {useState} from 'react';

import AgentList from './AgentList';

import {useAgents} from '../hooks/useAgents';

interface Props {
    email: string;
    onNewAgent: () => void;
    onSelectAgent: (agentId: string) => void;
    onOpenSettings: () => void;
    onNotConfigured: () => void;
}

const AgentListContainer = ({email, onNewAgent, onSelectAgent, onOpenSettings, onNotConfigured}: Props) => {
    const [includeArchived, setIncludeArchived] = useState(false);
    const {agents, loading, refreshing, error, refresh} = useAgents(includeArchived, onNotConfigured);

    return (
        <AgentList
            agents={agents}
            loading={loading}
            refreshing={refreshing}
            error={error}
            email={email}
            includeArchived={includeArchived}
            onToggleArchived={() => setIncludeArchived((value) => !value)}
            onRefresh={refresh}
            onNewAgent={onNewAgent}
            onSelectAgent={onSelectAgent}
            onOpenSettings={onOpenSettings}
        />
    );
};

export default AgentListContainer;
