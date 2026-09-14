import { describe, expect, it } from 'vitest';
import { buildNotificationChannelSpec, type NotificationChannel } from '../../src/pages/Notifications';

const base = {
  type: 'generic', url: '', events: [] as string[], nsFilter: '', throttle: '5m',
  enabled: true, deliveryPolicy: 'at-least-once', maxDeliveryAttempts: 7,
};

describe('notification channel delivery contract', () => {
  it('preserves urlFrom while editing retry policy without exposing a URL', () => {
    const channel: NotificationChannel = {
      metadata: { name: 'secure', namespace: 'aura-system' },
      spec: { type: 'generic', events: [], namespaceFilter: [], throttle: '5m', enabled: true, urlFrom: { name: 'webhook', key: 'url' } },
    };
    expect(buildNotificationChannelSpec(base, channel)).toMatchObject({
      deliveryPolicy: 'at-least-once', maxDeliveryAttempts: 7,
      urlFrom: { name: 'webhook', key: 'url' },
    });
  });

  it('forces one provider request for at-most-once delivery', () => {
    expect(buildNotificationChannelSpec({ ...base, deliveryPolicy: 'at-most-once', maxDeliveryAttempts: 20 }, null).maxDeliveryAttempts).toBe(1);
  });
});
