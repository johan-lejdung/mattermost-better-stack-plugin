import React from 'react';

import SecretURLDisplay from '@/components/secret_url_display';

const HealthURLDisplay: React.FC = () => (
    <SecretURLDisplay
        label='Uptime Check URL'
        helpText='Add this URL as an HTTP monitor in Better Stack to be alerted when this plugin can no longer reach Mattermost or the Better Stack API. It returns 200 while healthy and 503 otherwise.'
        pathPrefix='api/v1/health/'
    />
);

export default HealthURLDisplay;
