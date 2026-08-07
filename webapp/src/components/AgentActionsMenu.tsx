import React, {useState} from 'react';

interface Props {
    canCancel: boolean;
    archived: boolean;
    busy: boolean;
    onCancelRun: () => void;
    onToggleArchive: () => void;
    onDelete: () => void;
}

const AgentActionsMenu = ({canCancel, archived, busy, onCancelRun, onToggleArchive, onDelete}: Props) => {
    const [open, setOpen] = useState(false);
    const [confirmingDelete, setConfirmingDelete] = useState(false);

    const close = () => {
        setOpen(false);
        setConfirmingDelete(false);
    };

    const run = (action: () => void) => () => {
        close();
        action();
    };

    return (
        <div className='cursor-menu'>
            <button
                type='button'
                className='btn btn-tertiary btn-icon cursor-icon-button'
                onClick={() => setOpen((value) => !value)}
                disabled={busy}
                aria-label='Agent actions'
                title='Agent actions'
                aria-expanded={open}
            >
                <i className='icon icon-dots-horizontal'/>
            </button>

            {open ? (
                <React.Fragment>
                    <button
                        type='button'
                        className='cursor-menu__backdrop'
                        aria-label='Close menu'
                        onClick={close}
                    />
                    <div className='cursor-menu__list'>
                        {canCancel ? (
                            <button
                                type='button'
                                className='cursor-menu__item'
                                onClick={run(onCancelRun)}
                            >
                                <i className='icon icon-close-circle-outline'/>
                                {'Cancel run'}
                            </button>
                        ) : null}
                        <button
                            type='button'
                            className='cursor-menu__item'
                            onClick={run(onToggleArchive)}
                        >
                            <i className='icon icon-archive-outline'/>
                            {archived ? 'Unarchive agent' : 'Archive agent'}
                        </button>
                        {confirmingDelete ? (
                            <button
                                type='button'
                                className='cursor-menu__item cursor-menu__item--danger'
                                onClick={run(onDelete)}
                            >
                                <i className='icon icon-alert-outline'/>
                                {'Confirm permanent delete'}
                            </button>
                        ) : (
                            <button
                                type='button'
                                className='cursor-menu__item cursor-menu__item--danger'
                                onClick={() => setConfirmingDelete(true)}
                            >
                                <i className='icon icon-trash-can-outline'/>
                                {'Delete agent'}
                            </button>
                        )}
                    </div>
                </React.Fragment>
            ) : null}
        </div>
    );
};

export default AgentActionsMenu;
