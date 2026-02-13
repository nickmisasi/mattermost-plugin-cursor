import {Client4} from 'mattermost-redux/client';

import manifest from './manifest';
import type {Agent, AgentsResponse, FollowupRequest, StatusResponse} from './types';

const pluginApiBase = `/plugins/${manifest.id}/api/v1`;

class ClientClass {
    getAgents = async (): Promise<AgentsResponse> => {
        const url = `${pluginApiBase}/agents`;
        const response = await fetch(url, Client4.getOptions({
            method: 'GET',
        }));
        if (!response.ok) {
            throw new Error(`GET /agents failed: ${response.status}`);
        }
        return response.json();
    };

    getAgent = async (agentId: string): Promise<Agent> => {
        const url = `${pluginApiBase}/agents/${encodeURIComponent(agentId)}`;
        const response = await fetch(url, Client4.getOptions({
            method: 'GET',
        }));
        if (!response.ok) {
            throw new Error(`GET /agents/${agentId} failed: ${response.status}`);
        }
        return response.json();
    };

    addFollowup = async (agentId: string, message: string): Promise<StatusResponse> => {
        const url = `${pluginApiBase}/agents/${encodeURIComponent(agentId)}/followup`;
        const response = await fetch(url, Client4.getOptions({
            method: 'POST',
            body: JSON.stringify({message} as FollowupRequest),
        }));
        if (!response.ok) {
            throw new Error(`POST /agents/${agentId}/followup failed: ${response.status}`);
        }
        return response.json();
    };

    cancelAgent = async (agentId: string): Promise<StatusResponse> => {
        const url = `${pluginApiBase}/agents/${encodeURIComponent(agentId)}`;
        const response = await fetch(url, Client4.getOptions({
            method: 'DELETE',
        }));
        if (!response.ok) {
            throw new Error(`DELETE /agents/${agentId} failed: ${response.status}`);
        }
        return response.json();
    };
}

const Client = new ClientClass();
export default Client;
