// Mock for Jest tests - react-bootstrap is provided by Mattermost host at runtime.
import React from 'react';

export const Modal = ({show, children, className}: {
    show: boolean;
    onHide: () => void;
    children: React.ReactNode;
    className?: string;
}) => (show ? (
    <div
        data-testid='modal'
        className={className}
    >
        {children}
    </div>
) : null);

export const ModalHeader = ({children}: {children: React.ReactNode}) => <div data-testid='modal-header'>{children}</div>;
export const ModalTitle = ({children}: {children: React.ReactNode}) => <div data-testid='modal-title'>{children}</div>;
export const ModalBody = ({children}: {children: React.ReactNode}) => <div data-testid='modal-body'>{children}</div>;
export const ModalFooter = ({children}: {children: React.ReactNode}) => <div data-testid='modal-footer'>{children}</div>;
