import React, {useMemo, useRef, useState} from 'react';

import FooterBar from './FooterBar';
import RepoGroup from './RepoGroup';

import type {Agent} from '../types';
import {groupAgentsByRepository} from '../utils/grouping';

interface Props {
    agents: Agent[];
    loading: boolean;
    refreshing: boolean;
    loadingMore: boolean;
    hasMore: boolean;
    error: string;
    email: string;
    onRefresh: () => void;
    onLoadMore: () => void;
    onNewAgent: () => void;
    onSelectAgent: (agentId: string) => void;
    onOpenSettings: () => void;
}

const AgentList = ({
    agents,
    loading,
    refreshing,
    loadingMore,
    hasMore,
    error,
    email,
    onRefresh,
    onLoadMore,
    onNewAgent,
    onSelectAgent,
    onOpenSettings,
}: Props) => {
    const [searchOpen, setSearchOpen] = useState(false);
    const [query, setQuery] = useState('');
    const [includeArchived, setIncludeArchived] = useState(false);
    const searchInput = useRef<HTMLInputElement>(null);

    const groups = useMemo(
        () => groupAgentsByRepository(agents, {includeArchived, query}),
        [agents, includeArchived, query],
    );

    const archivedLabel = includeArchived ? 'Hide archived agents' : 'Show archived agents';

    const toggleSearch = () => {
        const opening = !searchOpen;
        setSearchOpen(opening);
        if (opening) {
            window.setTimeout(() => searchInput.current?.focus(), 0);
        } else {
            setQuery('');
        }
    };

    const renderBody = () => {
        if (loading) {
            return <p className='cursor-placeholder'>{'Loading your Cloud Agents…'}</p>;
        }
        if (groups.length) {
            return (
                <React.Fragment>
                    {groups.map((group) => (
                        <RepoGroup
                            key={group.key}
                            group={group}
                            onSelect={onSelectAgent}
                        />
                    ))}
                    {hasMore ? (
                        <button
                            type='button'
                            className='btn btn-tertiary cursor-load-more'
                            onClick={onLoadMore}
                            disabled={loadingMore}
                        >
                            {loadingMore ? 'Loading…' : 'Load more'}
                        </button>
                    ) : null}
                </React.Fragment>
            );
        }
        if (query.trim()) {
            return <p className='cursor-placeholder'>{`No agents match “${query.trim()}”.`}</p>;
        }
        return (
            <div className='cursor-empty'>
                <p className='cursor-placeholder'>{'You have no Cloud Agents yet.'}</p>
                <button
                    type='button'
                    className='btn btn-primary'
                    onClick={onNewAgent}
                >
                    {'Launch your first agent'}
                </button>
            </div>
        );
    };

    return (
        <div className='cursor-panel'>
            <div className='cursor-actions'>
                <button
                    type='button'
                    className='cursor-action-row'
                    onClick={onNewAgent}
                >
                    <i className='icon icon-pencil-outline'/>
                    {'New Agent'}
                </button>
                {searchOpen ? (
                    <div className='cursor-search'>
                        <i className='icon icon-magnify'/>
                        <input
                            ref={searchInput}
                            className='cursor-search__input'
                            type='text'
                            placeholder='Search agents'
                            aria-label='Search agents'
                            value={query}
                            onChange={(event) => setQuery(event.target.value)}
                        />
                        <button
                            type='button'
                            className='btn btn-tertiary btn-icon cursor-icon-button'
                            onClick={toggleSearch}
                            aria-label='Close search'
                            title='Close search'
                        >
                            <i className='icon icon-close'/>
                        </button>
                    </div>
                ) : (
                    <button
                        type='button'
                        className='cursor-action-row'
                        onClick={toggleSearch}
                    >
                        <i className='icon icon-magnify'/>
                        {'Search'}
                    </button>
                )}
            </div>

            <div className='cursor-section-header'>
                <span>{'Repositories'}</span>
                <span className='cursor-section-header__actions'>
                    <button
                        type='button'
                        className={`btn btn-tertiary btn-icon cursor-icon-button${includeArchived ? ' cursor-icon-button--active' : ''}`}
                        onClick={() => setIncludeArchived((value) => !value)}
                        aria-pressed={includeArchived}
                        aria-label={archivedLabel}
                        title={archivedLabel}
                    >
                        <i className='icon icon-archive-outline'/>
                    </button>
                    <button
                        type='button'
                        className='btn btn-tertiary btn-icon cursor-icon-button'
                        onClick={onRefresh}
                        disabled={refreshing}
                        aria-label='Refresh agents'
                        title='Refresh agents'
                    >
                        <i className='icon icon-refresh'/>
                    </button>
                </span>
            </div>

            {error ? (
                <p
                    className='cursor-error cursor-error--banner'
                    role='alert'
                >
                    {error}
                </p>
            ) : null}

            <div className='cursor-scroll'>
                {renderBody()}
            </div>

            <FooterBar
                email={email}
                onOpenSettings={onOpenSettings}
            />
        </div>
    );
};

export default AgentList;
