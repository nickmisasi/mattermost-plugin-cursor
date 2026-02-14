import {Client4} from 'mattermost-redux/client';

import manifest from './manifest';
import type {Agent, AgentsResponse, FollowupRequest, StatusResponse, Workflow} from './types';

const pluginApiBase = `/plugins/${manifest.id}/api/v1`;

class ClientClass {
    getAgents = async (archived?: boolean): Promise<AgentsResponse> => {
        const params = archived ? '?archived=true' : '';
        const url = `${pluginApiBase}/agents${params}`;
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

    archiveAgent = async (agentId: string): Promise<StatusResponse> => {
        const url = `${pluginApiBase}/agents/${encodeURIComponent(agentId)}/archive`;
        const response = await fetch(url, Client4.getOptions({
            method: 'POST',
        }));
        if (!response.ok) {
            throw new Error(`POST /agents/${agentId}/archive failed: ${response.status}`);
        }
        return response.json();
    };

    unarchiveAgent = async (agentId: string): Promise<StatusResponse> => {
        const url = `${pluginApiBase}/agents/${encodeURIComponent(agentId)}/unarchive`;
        const response = await fetch(url, Client4.getOptions({
            method: 'POST',
        }));
        if (!response.ok) {
            throw new Error(`POST /agents/${agentId}/unarchive failed: ${response.status}`);
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

    getWorkflow = async (workflowId: string): Promise<Workflow> => {
        const url = `${pluginApiBase}/workflows/${encodeURIComponent(workflowId)}`;
        const response = await fetch(url, Client4.getOptions({
            method: 'GET',
        }));
        if (!response.ok) {
            throw new Error(`GET /workflows/${workflowId} failed: ${response.status}`);
        }
        return response.json();
    };
}

const Client = new ClientClass();
export default Client;
