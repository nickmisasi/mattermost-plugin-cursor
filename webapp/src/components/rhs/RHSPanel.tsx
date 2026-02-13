import React, {useEffect} from 'react';
import {useSelector, useDispatch} from 'react-redux';

import {getSelectedAgent, getAgentsList, getIsLoading} from '../../selectors';
import {fetchAgents, selectAgent} from '../../actions';
import AgentList from './AgentList';
import AgentDetail from './AgentDetail';

import '../common/styles.css';

const RHSPanel: React.FC = () => {
    const dispatch = useDispatch();
    const agents = useSelector(getAgentsList);
    const selectedAgent = useSelector(getSelectedAgent);
    const isLoading = useSelector(getIsLoading);

    useEffect(() => {
        dispatch(fetchAgents() as any);
    }, [dispatch]);

    if (selectedAgent) {
        return (
            <AgentDetail
                agent={selectedAgent}
                onBack={() => dispatch(selectAgent(null))}
            />
        );
    }

    return (
        <AgentList
            agents={agents}
            isLoading={isLoading}
            onSelectAgent={(id) => dispatch(selectAgent(id))}
        />
    );
};

export default RHSPanel;
