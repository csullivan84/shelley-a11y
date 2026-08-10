import type { NotificationEvent } from "../../../types";

function notifTitle(hostname: string, slug: string): string {
  if (hostname && slug) return `${hostname}: ${slug}`;
  return hostname || slug || "Shelley";
}

function notificationTarget(conversationURL?: string): string {
  if (!conversationURL) return window.location.href.split("#")[0];
  try {
    const parsed = new URL(conversationURL, window.location.href);
    return `${window.location.origin}${parsed.pathname}${parsed.search}`;
  } catch {
    return window.location.href.split("#")[0];
  }
}

function focusConversation(notification: Notification, conversationURL?: string): void {
  notification.onclick = () => {
    window.location.assign(`${notificationTarget(conversationURL)}#turn-completion-summary`);
    window.focus();
    notification.close();
  };
}

function notificationBody(event: NotificationEvent): string {
  const raw =
    event.type === "agent_error"
      ? event.payload?.error_message || "Agent error"
      : event.payload?.final_response || "Agent finished";
  const compact = String(raw).replace(/\s+/g, " ").trim();
  const model = event.payload?.model ? `${event.payload.model}: ` : "";
  const body = model + compact;
  return body.length > 320 ? `${body.slice(0, 319)}…` : body;
}

function showBrowserNotification(event: NotificationEvent, force: boolean): boolean {
  if (!force && !document.hidden && document.hasFocus()) return false;
  if (typeof Notification === "undefined") return false;
  if (Notification.permission !== "granted") return false;

  const hostname = event.payload?.hostname || window.__SHELLEY_INIT__?.hostname || "localhost";
  const slug = event.payload?.conversation_title || "";

  switch (event.type) {
    case "agent_done": {
      const notification = new Notification(notifTitle(hostname, slug), {
        body: notificationBody(event),
        tag: `shelley-done-${event.conversation_id}`,
      });
      focusConversation(notification, event.payload?.conversation_url);
      return true;
    }
    case "agent_error": {
      const notification = new Notification(notifTitle(hostname, "error"), {
        body: notificationBody(event),
        tag: `shelley-error-${event.conversation_id}`,
      });
      focusConversation(notification, event.payload?.conversation_url);
      return true;
    }
  }
  return false;
}

export function browserNotificationHandler(event: NotificationEvent): void {
  showBrowserNotification(event, false);
}

export function sendTestBrowserNotification(): boolean {
  return showBrowserNotification(
    {
      type: "agent_done",
      conversation_id: "browser-notification-test",
      timestamp: new Date().toISOString(),
      payload: {
        hostname: window.__SHELLEY_INIT__?.hostname || "Shelley",
        conversation_title: "test",
        final_response: "Browser completion notifications are working.",
        conversation_url: window.location.href,
      },
    },
    true,
  );
}
