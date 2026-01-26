import type { CallbackPayload } from "../types";

export async function sendWebhook(
  url: string,
  payload: CallbackPayload
): Promise<boolean> {
  try {
    const response = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
      signal: AbortSignal.timeout(10000),
    });

    if (!response.ok) {
      console.error(
        `[webhook] Failed to send webhook to ${url}: ${response.status} ${response.statusText}`
      );
      return false;
    }

    console.log(`[webhook] Successfully sent ${payload.status} webhook to ${url}`);
    return true;
  } catch (error) {
    console.error(`[webhook] Error sending webhook to ${url}:`, error);
    return false;
  }
}
