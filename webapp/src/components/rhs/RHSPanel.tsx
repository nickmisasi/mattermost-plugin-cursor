import React, {useCallback, useEffect, useState} from 'react';
import {useSelector, useDispatch} from 'react-redux';

import AgentDetail from './AgentDetail';
import AgentList from './AgentList';

import {fetchAgents, selectAgent} from '../../actions';
import {getSelectedAgent, getAgentsList, getIsLoading} from '../../selectors';

import '../common/styles.css';

const RHSPanel: React.FC = () => {
    const dispatch = useDispatch();
    const agents = useSelector(getAgentsList);
    const selectedAgent = useSelector(getSelectedAgent);
    const isLoading = useSelector(getIsLoading);
    const [showArchived, setShowArchived] = useState(false);

    useEffect(() => {
        dispatch(fetchAgents(showArchived ? true : undefined) as any);
    }, [dispatch, showArchived]);

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
