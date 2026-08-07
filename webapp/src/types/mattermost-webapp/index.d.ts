import type {ComponentType, ReactNode} from 'react';
import type {AnyAction} from 'redux';

export interface RightHandSidebarRegistration {
    id: string;
    showRHSPlugin: AnyAction;
    hideRHSPlugin: AnyAction;
    toggleRHSPlugin: AnyAction;
}

export interface PluginRegistry {
    registerRightHandSidebarComponent(
        component: ComponentType,
        title: ReactNode,
    ): RightHandSidebarRegistration;

    registerAppBarComponent(
        iconUrl: string,
        action: () => void,
        tooltipText: ReactNode,
        supportedProductIds?: string[] | string | null,
    ): string;
}
