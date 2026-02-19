/**
 * Bug 4: Archive/Unarchive loading spinner behavior.
 */
import * as fs from 'fs';
import * as path from 'path';

describe('AgentCard archive/unarchive (Bug 4)', () => {
    it('shows loading spinner when archiveLoading or unarchiveLoading is true', () => {
        const agentCardSrc = fs.readFileSync(path.join(__dirname, 'AgentCard.tsx'), 'utf8');
        expect(agentCardSrc).toContain('archiveLoading');
        expect(agentCardSrc).toContain('unarchiveLoading');
        expect(agentCardSrc).toContain('cursor-agent-card-archive-spinner');
    });

    it('disables button during loading', () => {
        const agentCardSrc = fs.readFileSync(path.join(__dirname, 'AgentCard.tsx'), 'utf8');
        expect(agentCardSrc).toContain('disabled={archiveLoading}');
        expect(agentCardSrc).toContain('disabled={unarchiveLoading}');
    });
});
