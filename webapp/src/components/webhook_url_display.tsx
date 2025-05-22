import React, {useState} from 'react';

// useSelector is available as a webpack external via react-redux
const {useSelector} = (window as any).ReactRedux;

import manifest from '@/manifest';

const baseStyles: Record<string, React.CSSProperties> = {
    wrapper: {
        marginBottom: '20px',
    },
    label: {
        display: 'block',
        fontWeight: 600,
        marginBottom: '6px',
        fontSize: '14px',
    },
    helpText: {
        display: 'block',
        color: 'var(--center-channel-color-56, #888)',
        fontSize: '12px',
        marginBottom: '8px',
    },
    row: {
        display: 'flex',
        alignItems: 'stretch',
        gap: '8px',
    },
    input: {
        flex: 1,
        fontFamily: 'monospace',
        fontSize: '13px',
        padding: '6px 10px',
        border: '1px solid var(--center-channel-color-24, #ccc)',
        borderRadius: '4px',
        background: 'var(--center-channel-bg, #fff)',
        color: 'var(--center-channel-color, #333)',
        cursor: 'text',
    },
    button: {
        padding: '6px 14px',
        border: '1px solid var(--button-bg, #166de0)',
        borderRadius: '4px',
        background: 'transparent',
        color: 'var(--button-bg, #166de0)',
        cursor: 'pointer',
        fontSize: '13px',
        whiteSpace: 'nowrap' as const,
        flexShrink: 0,
    },
    notice: {
        color: 'var(--center-channel-color-56, #888)',
        fontSize: '13px',
    },
};

const WebhookURLDisplay: React.FC = () => {
    const [copied, setCopied] = useState(false);

    const token: string = useSelector(
        (state: any) =>
            state?.entities?.admin?.config?.PluginSettings?.Plugins?.[manifest.id]?.webhooktoken ?? '',
    );

    const url = token
        ? `${window.location.origin}/plugins/${manifest.id}/api/v1/webhook/${token}`
        : '';

    const handleCopy = () => {
        if (!url) {
            return;
        }
        navigator.clipboard.writeText(url).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        });
    };

    return (
        <div style={baseStyles.wrapper}>
            <label style={baseStyles.label}>{'Webhook URL'}</label>
            <span style={baseStyles.helpText}>
                {'Configure this URL as the webhook destination in Better Stack. The secret token is embedded in the path.'}
            </span>
            {!url && (
                <span style={baseStyles.notice}>
                    {'Save the plugin settings first to generate the webhook token.'}
                </span>
            )}
            {url && (
                <div style={baseStyles.row}>
                    <input
                        style={baseStyles.input}
                        type='text'
                        readOnly={true}
                        value={url}
                        onClick={(e) => (e.target as HTMLInputElement).select()}
                    />
                    <button
                        style={baseStyles.button}
                        onClick={handleCopy}
                        type='button'
                    >
                        {copied ? 'Copied!' : 'Copy'}
                    </button>
                </div>
            )}
        </div>
    );
};

export default WebhookURLDisplay;
