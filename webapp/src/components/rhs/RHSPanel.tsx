import React, {useCallback, useEffect, useState} from 'react';
import {useSelector, useDispatch} from 'react-redux';

import AgentDetail from './AgentDetail';
import AgentList from './AgentList';

import {fetchAgent, fetchAgents, selectAgent} from '../../actions';
import {getSelectedAgent, getSelectedAgentId, getAgentsList, getIsLoading} from '../../selectors';

import '../common/styles.css';

const RHSPanel: React.FC = () => {
    const dispatch = useDispatch();
    const agents = useSelector(getAgentsList);
    const selectedAgentId = useSelector(getSelectedAgentId);
    const selectedAgent = useSelector(getSelectedAgent);
    const isLoading = useSelector(getIsLoading);
    const [showArchived, setShowArchived] = useState(false);

    useEffect(() => {
        dispatch(fetchAgents(showArchived ? true : undefined) as any);
    }, [dispatch, showArchived]);

    // When selecting via "View Agent Details" from a post, agent may not be in state yet; fetch it.
    useEffect(() => {
        if (selectedAgentId && !selectedAgent) {
            dispatch(fetchAgent(selectedAgentId) as any);
        }
    }, [selectedAgentId, selectedAgent, dispatch]);

    const handleTabChange = useCallback((archived: boolean) => {
        setShowArchived(archived);
    }, []);

    if (selectedAgent) {
        return (
            <AgentDetail
                agent={selectedAgent}
                onBack={() => dispatch(selectAgent(null) as any)}
            />
        );
    }

    return (
        <AgentList
            agents={agents}
            isLoading={isLoading}
            onSelectAgent={(id) => dispatch(selectAgent(id) as any)}
            showArchived={showArchived}
            onTabChange={handleTabChange}
        />
    );
};

export default RHSPanel;
