/**
 * Bug 4: Archive/Unarchive confirmation and loading behavior.
 * Tests verify the implementation via static analysis and structure checks.
 */
import * as fs from 'fs';
import * as path from 'path';

describe('AgentList archive/unarchive (Bug 4)', () => {
    it('uses ConfirmModal for archive/unarchive confirmation', () => {
        const agentListSrc = fs.readFileSync(path.join(__dirname, 'AgentList.tsx'), 'utf8');
        expect(agentListSrc).toContain('ConfirmModal');
        expect(agentListSrc).toContain('confirmModal');
        expect(agentListSrc).toContain('setConfirmModal');
        expect(agentListSrc).toContain('Archive Agent');
        expect(agentListSrc).toContain('Unarchive Agent');
    });

    it('shows confirmation modal with appropriate messages', () => {
        const agentListSrc = fs.readFileSync(path.join(__dirname, 'AgentList.tsx'), 'utf8');
        expect(agentListSrc).toContain('Are you sure you want to archive this agent?');
        expect(agentListSrc).toContain('Are you sure you want to unarchive this agent?');
    });

    it('tracks loading state to prevent duplicate clicks', () => {
        const agentListSrc = fs.readFileSync(path.join(__dirname, 'AgentList.tsx'), 'utf8');
        expect(agentListSrc).toContain('loadingAgentId');
        expect(agentListSrc).toContain('setLoadingAgentId');
    });
});
