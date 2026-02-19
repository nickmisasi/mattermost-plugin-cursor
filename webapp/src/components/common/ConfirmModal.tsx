import React from 'react';
import {Modal, ModalFooter, ModalHeader, ModalTitle, ModalBody} from 'react-bootstrap';

interface Props {
    show: boolean;
    title: string;
    message: string;
    confirmText: string;
    confirmClassName?: string;
    onConfirm: () => void;
    onCancel: () => void;
    onExited?: () => void;
}

/**
 * Confirmation modal matching Mattermost look/feel.
 * Uses CSS variables for theming. Renders as a standard Mattermost-style modal
 * with Cancel (tertiary) and Confirm (primary) buttons.
 */
const ConfirmModal: React.FC<Props> = ({
    show,
    title,
    message,
    confirmText,
    confirmClassName = 'btn-primary',
    onConfirm,
    onCancel,
    onExited,
}) => {
    return (
        <Modal
            show={show}
            onHide={onCancel}
            onExited={onExited}
            centered={true}
            className='cursor-confirm-modal'
        >
            <ModalHeader closeButton={false}>
                <ModalTitle>{title}</ModalTitle>
            </ModalHeader>
            <ModalBody>{message}</ModalBody>
            <ModalFooter>
                <button
                    type='button'
                    className='btn btn-tertiary'
                    onClick={onCancel}
                >
                    {'Cancel'}
                </button>
                <button
                    type='button'
                    className={`btn ${confirmClassName}`}
                    onClick={onConfirm}
                >
                    {confirmText}
                </button>
            </ModalFooter>
        </Modal>
    );
};

export default ConfirmModal;
