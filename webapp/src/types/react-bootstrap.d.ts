declare module 'react-bootstrap' {
    import type {FC, ReactNode} from 'react';

    export interface ModalProps {
        show?: boolean;
        onHide?: () => void;
        onExited?: () => void;
        children?: ReactNode;
        className?: string;
    }

    export const Modal: FC<ModalProps>;
    export const ModalHeader: FC<{children?: ReactNode; closeButton?: boolean}>;
    export const ModalTitle: FC<{children?: ReactNode}>;
    export const ModalBody: FC<{children?: ReactNode}>;
    export const ModalFooter: FC<{children?: ReactNode}>;
}
