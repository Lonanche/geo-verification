import type { CallbackPayload } from "../types";

async function generateSignature(
  payload: string,
  secret: string
): Promise<string> {
  const encoder = new TextEncoder();
  const key = await crypto.subtle.importKey(
    "raw",
    encoder.encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"]
  );
  const signature = await crypto.subtle.sign("HMAC", key, encoder.encode(payload));
  const hashArray = Array.from(new Uint8Array(signature));
  return hashArray.map((b) => b.toString(16).padStart(2, "0")).join("");
}

export async function sendWebhook(
  url: string,
  payload: CallbackPayload,
  secret?: string
): Promise<boolean> {
  try {
    const body = JSON.stringify(payload);
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };

    if (secret) {
      const signature = await generateSignature(body, secret);
      headers["X-Webhook-Signature"] = `sha256=${signature}`;
    }

    const response = await fetch(url, {
      method: "POST",
      headers,
      body,
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
