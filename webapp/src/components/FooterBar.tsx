import React from 'react';

interface Props {
    email: string;
    onOpenSettings: () => void;
}

const FooterBar = ({email, onOpenSettings}: Props) => (
    <footer className='cursor-footer'>
        <span
            className='cursor-footer__identity'
            title={email}
        >
            <i className='icon icon-account-outline'/>
            <span className='cursor-footer__email'>{email || 'Cursor account'}</span>
        </span>
        <button
            type='button'
            className='btn btn-tertiary btn-icon cursor-icon-button'
            onClick={onOpenSettings}
            aria-label='Cursor connection settings'
            title='Cursor connection settings'
        >
            <i className='icon icon-settings-outline'/>
        </button>
    </footer>
);

export default FooterBar;
