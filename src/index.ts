import type {
  Env,
  StartVerificationRequest,
  StartVerificationResponse,
  SessionStatusResponse,
  ErrorResponse,
  Session,
  RateLimitStatus,
} from "./types";
import { validateCallbackUrl } from "./utils/validation";

export { VerificationManager } from "./durable-objects/VerificationManager";
export { UserRateLimiter } from "./durable-objects/UserRateLimiter";

function corsHeaders(): HeadersInit {
  return {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
    "Access-Control-Allow-Headers": "Content-Type, X-API-Key",
  };
}

function validateApiKey(request: Request, env: Env): boolean {
  if (!env.API_KEY) return true;
  const apiKey = request.headers.get("X-API-Key");
  return apiKey === env.API_KEY;
}

function jsonResponse<T>(data: T, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      "Content-Type": "application/json",
      ...corsHeaders(),
    },
  });
}

function errorResponse(error: string, status: number): Response {
  const body: ErrorResponse = { error };
  return jsonResponse(body, status);
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (request.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: corsHeaders() });
    }

    if (url.pathname === "/health" && request.method === "GET") {
      return jsonResponse({ status: "ok" });
    }

    if (!validateApiKey(request, env)) {
      return errorResponse("Unauthorized", 401);
    }

    if (url.pathname === "/api/v1/verify/start" && request.method === "POST") {
      return handleStartVerification(request, env);
    }

    const statusMatch = url.pathname.match(/^\/api\/v1\/verify\/status\/(.+)$/);
    if (statusMatch && request.method === "GET") {
      return handleGetStatus(statusMatch[1], env);
    }

    return errorResponse("Not Found", 404);
  },
};

async function handleStartVerification(
  request: Request,
  env: Env
): Promise<Response> {
  let body: StartVerificationRequest;
  try {
    body = (await request.json()) as StartVerificationRequest;
  } catch {
    return errorResponse("Invalid JSON body", 400);
  }

  if (!body.user_id || typeof body.user_id !== "string") {
    return errorResponse("user_id is required", 400);
  }

  const userId = body.user_id.trim();
  if (!userId) {
    return errorResponse("user_id cannot be empty", 400);
  }

  if (body.callback_url) {
    const validation = validateCallbackUrl(
      body.callback_url,
      env.ALLOWED_CALLBACK_HOSTS
    );
    if (!validation.valid) {
      return errorResponse(validation.error!, 400);
    }
  }

  const rateLimiterId = env.USER_RATE_LIMITER.idFromName(userId);
  const rateLimiter = env.USER_RATE_LIMITER.get(rateLimiterId);

  const maxTokens = env.RATE_LIMIT_PER_HOUR || "3";
  const rateLimitResponse = await rateLimiter.fetch(
    new Request(`http://internal/check?maxTokens=${maxTokens}`, {
      method: "POST",
    })
  );

  const rateLimitStatus = (await rateLimitResponse.json()) as RateLimitStatus;
  if (!rateLimitStatus.allowed) {
    const resetDate = new Date(rateLimitStatus.resetAt);
    return errorResponse(
      `Rate limit exceeded. Try again after ${resetDate.toISOString()}`,
      429
    );
  }

  const managerId = env.VERIFICATION_MANAGER.idFromName("singleton");
  const manager = env.VERIFICATION_MANAGER.get(managerId);

  const sessionResponse = await manager.fetch(
    new Request("http://internal/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        userId,
        callbackUrl: body.callback_url,
      }),
    })
  );

  if (!sessionResponse.ok) {
    return errorResponse("Failed to create verification session", 500);
  }

  const session = (await sessionResponse.json()) as Session;

  const response: StartVerificationResponse = {
    session_id: session.id,
    verification_code: session.code,
    expires_at: new Date(session.expiresAt).toISOString(),
  };

  return jsonResponse(response);
}

async function handleGetStatus(sessionId: string, env: Env): Promise<Response> {
  const managerId = env.VERIFICATION_MANAGER.idFromName("singleton");
  const manager = env.VERIFICATION_MANAGER.get(managerId);

  const sessionResponse = await manager.fetch(
    new Request(`http://internal/session/${sessionId}`, {
      method: "GET",
    })
  );

  if (!sessionResponse.ok) {
    if (sessionResponse.status === 404) {
      return errorResponse("Session not found or expired", 404);
    }
    return errorResponse("Failed to fetch session", 500);
  }

  const session = (await sessionResponse.json()) as Session;

  const response: SessionStatusResponse = {
    session_id: session.id,
    user_id: session.userId,
    verified: session.verified,
    expires_at: new Date(session.expiresAt).toISOString(),
    created_at: new Date(session.createdAt).toISOString(),
  };

  return jsonResponse(response);
}
