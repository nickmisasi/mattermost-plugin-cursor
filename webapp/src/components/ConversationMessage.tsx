import React, {useMemo} from 'react';

import type {ConversationMessage as Message} from '../types';
import {parseSegments} from '../utils/segments';

interface Props {
    message: Message;
}

const ConversationMessage = ({message}: Props) => {
    const segments = useMemo(() => parseSegments(message.text), [message.text]);

    return (
        <article className={`cursor-message cursor-message--${message.role}`}>
            <span className='cursor-message__author'>{message.role === 'user' ? 'You' : 'Cursor'}</span>
            {segments.map((segment, index) => (segment.type === 'code' ? (
                <pre
                    // eslint-disable-next-line react/no-array-index-key
                    key={index}
                    className='cursor-message__code'
                >
                    {segment.content}
                </pre>
            ) : (
                <p
                    // eslint-disable-next-line react/no-array-index-key
                    key={index}
                    className='cursor-message__text'
                >
                    {segment.content}
                </p>
            )))}
        </article>
    );
};

export default ConversationMessage;
