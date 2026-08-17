import React from 'react';

import SecretURLDisplay from '@/components/secret_url_display';

const WebhookURLDisplay: React.FC = () => (
    <SecretURLDisplay
        label='Webhook URL'
        helpText='Configure this URL as the webhook destination in Better Stack. The secret token is embedded in the path.'
        pathPrefix='api/v1/webhook/'
    />
);

export default WebhookURLDisplay;
