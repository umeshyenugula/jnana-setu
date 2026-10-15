// Documented postMessage schema between the host page, the loader, and the
// widget iframe. Every inbound message is validated for exact origin and
// payload shape; anything else is ignored.
export const WIDGET_MESSAGE = "jnana-widget" as const;

export type WidgetOutbound =
  | { type: "jnana-opened" }
  | { type: "jnana-closed" }
  | { type: "jnana-resize"; height: number };

export type WidgetInbound = { type: "jnana-open" } | { type: "jnana-close" } | { type: "jnana-toggle" };

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

// guardInbound accepts only the documented host→iframe commands from the
// exact expected origin. It never accepts HTML, commands with arguments, or
// wildcard origins.
export function guardInbound(event: MessageEvent, expectedOrigin: string): WidgetInbound | null {
  if (event.origin !== expectedOrigin) return null;
  if (!isRecord(event.data)) return null;
  if (event.data.namespace !== WIDGET_MESSAGE) return null;
  const type = event.data.type;
  if (type === "jnana-open" || type === "jnana-close" || type === "jnana-toggle") {
    return { type };
  }
  return null;
}

export function outboundMessage(message: WidgetOutbound): { namespace: typeof WIDGET_MESSAGE } & WidgetOutbound {
  return { namespace: WIDGET_MESSAGE, ...message };
}

// guardOutbound validates messages the loader receives from the iframe.
export function guardOutbound(event: MessageEvent, expectedOrigin: string): WidgetOutbound | null {
  if (event.origin !== expectedOrigin) return null;
  if (!isRecord(event.data)) return null;
  if (event.data.namespace !== WIDGET_MESSAGE) return null;
  const type = event.data.type;
  if (type === "jnana-opened" || type === "jnana-closed") return { type };
  if (type === "jnana-resize" && typeof event.data.height === "number" && event.data.height > 0 && event.data.height < 2000) {
    return { type, height: event.data.height };
  }
  return null;
}
