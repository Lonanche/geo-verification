export function validateCallbackUrl(
  url: string,
  allowedHosts: string
): { valid: boolean; error?: string } {
  if (!url) {
    return { valid: true };
  }

  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return { valid: false, error: "Invalid callback URL format" };
  }

  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return { valid: false, error: "Callback URL must use HTTP or HTTPS" };
  }

  const allowedHostsList = allowedHosts.split(",").map((h) => h.trim().toLowerCase());
  const hostname = parsed.hostname.toLowerCase();

  const isAllowed = allowedHostsList.some((pattern) => {
    if (pattern.startsWith("*.")) {
      const domain = pattern.slice(2);
      return hostname === domain || hostname.endsWith("." + domain);
    }
    return hostname === pattern;
  });

  if (!isAllowed) {
    return {
      valid: false,
      error: `Callback host '${hostname}' is not in allowed hosts list`,
    };
  }

  return { valid: true };
}
