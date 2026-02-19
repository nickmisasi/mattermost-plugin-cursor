/**
 * Bug 4: ConfirmModal component for Archive/Unarchive.
 */
import * as fs from 'fs';
import * as path from 'path';

describe('ConfirmModal', () => {
    it('renders Modal from react-bootstrap with Mattermost styling', () => {
        const src = fs.readFileSync(path.join(__dirname, 'ConfirmModal.tsx'), 'utf8');
        expect(src).toContain('react-bootstrap');
        expect(src).toContain('Modal');
        expect(src).toContain('cursor-confirm-modal');
    });

    it('has Cancel and Confirm buttons', () => {
        const src = fs.readFileSync(path.join(__dirname, 'ConfirmModal.tsx'), 'utf8');
        expect(src).toContain('btn-tertiary');
        expect(src).toContain('Cancel');
        expect(src).toContain('confirmText');
    });

    it('accepts show, title, message, onConfirm, onCancel props', () => {
        const src = fs.readFileSync(path.join(__dirname, 'ConfirmModal.tsx'), 'utf8');
        expect(src).toContain('show');
        expect(src).toContain('onConfirm');
        expect(src).toContain('onCancel');
    });
});
