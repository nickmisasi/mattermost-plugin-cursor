import React from 'react';

interface Props {
    href: string;
    children: React.ReactNode;
    className?: string;
    onClick?: (e: React.MouseEvent<HTMLAnchorElement>) => void;
}

const ExternalLink: React.FC<Props> = ({href, children, className, onClick}) => (
    <a // eslint-disable-line @mattermost/use-external-link
        href={href}
        target='_blank'
        rel='noopener noreferrer'
        className={className}
        onClick={onClick}
    >
        {children}
    </a>
);

export default ExternalLink;
