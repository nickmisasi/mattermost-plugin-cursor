import React from 'react';

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
    const {agents, loading, refreshing, loadingMore, nextCursor, error, refresh, loadMore} = useAgents(onNotConfigured);

    return (
        <AgentList
            agents={agents}
            loading={loading}
            refreshing={refreshing}
            loadingMore={loadingMore}
            hasMore={Boolean(nextCursor)}
            error={error}
            email={email}
            onRefresh={refresh}
            onLoadMore={loadMore}
            onNewAgent={onNewAgent}
            onSelectAgent={onSelectAgent}
            onOpenSettings={onOpenSettings}
        />
    );
};

export default AgentListContainer;
