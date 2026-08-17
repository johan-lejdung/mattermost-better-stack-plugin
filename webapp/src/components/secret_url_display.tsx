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

type Props = {

    // Heading shown above the URL field.
    label: string;

    // Explanation of what the URL is for.
    helpText: string;

    // Path under the plugin base URL, with the secret token appended, e.g. 'api/v1/webhook/'.
    pathPrefix: string;
}

// SecretURLDisplay renders a read-only, copyable URL built from the plugin's webhook
// secret token. Used for both the incident webhook URL and the uptime check URL.
const SecretURLDisplay: React.FC<Props> = ({label, helpText, pathPrefix}) => {
    const [copied, setCopied] = useState(false);

    const token: string = useSelector(
        (state: any) =>
            state?.entities?.admin?.config?.PluginSettings?.Plugins?.[manifest.id]?.webhooktoken ?? '',
    );

    const url = token ?
        `${window.location.origin}/plugins/${manifest.id}/${pathPrefix}${token}` :
        '';

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
            <label style={baseStyles.label}>{label}</label>
            <span style={baseStyles.helpText}>{helpText}</span>
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

export default SecretURLDisplay;
