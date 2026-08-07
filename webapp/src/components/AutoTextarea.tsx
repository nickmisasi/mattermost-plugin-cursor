import React, {useCallback, useEffect, useRef} from 'react';

interface Props {
    id?: string;
    className?: string;
    value: string;
    placeholder?: string;
    minRows?: number;
    maxRows?: number;
    autoFocus?: boolean;
    disabled?: boolean;
    onChange: (value: string) => void;
    onSubmit?: () => void;
}

const LINE_HEIGHT_FALLBACK = 20;

/**
 * Textarea that grows with its content up to `maxRows`.
 */
const AutoTextarea = ({
    id,
    className,
    value,
    placeholder,
    minRows = 2,
    maxRows = 12,
    autoFocus = false,
    disabled = false,
    onChange,
    onSubmit,
}: Props) => {
    const ref = useRef<HTMLTextAreaElement>(null);

    const resize = useCallback(() => {
        const element = ref.current;
        if (!element) {
            return;
        }

        const computed = window.getComputedStyle(element);
        const lineHeight = parseFloat(computed.lineHeight) || LINE_HEIGHT_FALLBACK;
        const padding = parseFloat(computed.paddingTop) + parseFloat(computed.paddingBottom) || 0;

        element.style.height = 'auto';
        const max = (lineHeight * maxRows) + padding;
        element.style.height = `${Math.min(element.scrollHeight, max)}px`;
        element.style.overflowY = element.scrollHeight > max ? 'auto' : 'hidden';
    }, [maxRows]);

    useEffect(resize, [resize, value]);

    useEffect(() => {
        if (autoFocus) {
            ref.current?.focus();
        }
    }, [autoFocus]);

    const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
        if (onSubmit && event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            onSubmit();
        }
    };

    return (
        <textarea
            id={id}
            ref={ref}
            className={className}
            rows={minRows}
            value={value}
            placeholder={placeholder}
            disabled={disabled}
            onChange={(event) => onChange(event.target.value)}
            onKeyDown={handleKeyDown}
        />
    );
};

export default AutoTextarea;
