import type { Session, CallbackPayload } from "../types";
import { GeoGuessrClient } from "../services/geoguessr-client";
import { sendWebhook } from "../services/webhook";
import {
  generateVerificationCode,
  generateSessionId,
} from "../utils/crypto";

interface VerificationManagerEnv {
  GEOGUESSR_NCFA_TOKEN: string;
  CODE_EXPIRY_MINUTES: string;
  RATE_LIMIT_PER_HOUR: string;
  API_KEY?: string;
}

interface RateLimitState {
  tokens: number;
  lastRefill: number;
}

export class VerificationManager implements DurableObject {
  private state: DurableObjectState;
  private env: VerificationManagerEnv;
  private client: GeoGuessrClient | null = null;

  constructor(state: DurableObjectState, env: VerificationManagerEnv) {
    this.state = state;
    this.env = env;
  }

  private getClient(): GeoGuessrClient {
    if (!this.client) {
      this.client = new GeoGuessrClient(this.env.GEOGUESSR_NCFA_TOKEN);
    }
    return this.client;
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const method = request.method;
    const pathname = url.pathname;

    if (method === "POST" && pathname === "/session") {
      return this.createSession(request);
    }

    if (pathname.startsWith("/session/")) {
      const sessionId = pathname.split("/")[2];
      if (method === "GET") return this.getSession(sessionId);
      if (method === "DELETE") return this.deleteSession(sessionId);
    }

    if (method === "POST" && pathname === "/start-alarm") {
      return this.startAlarm();
    }

    return new Response("Not Found", { status: 404 });
  }

  private async createSession(request: Request): Promise<Response> {
    const body = (await request.json()) as {
      userId: string;
      callbackUrl?: string;
    };

    const rateLimitResult = await this.checkRateLimit(body.userId);
    if (!rateLimitResult.allowed) {
      const resetDate = new Date(rateLimitResult.resetAt).toISOString();
      return new Response(
        JSON.stringify({ error: `Rate limit exceeded. Try again after ${resetDate}` }),
        { status: 429, headers: { "Content-Type": "application/json" } }
      );
    }

    const existingSessionId = await this.state.storage.get<string>(
      `session_by_user:${body.userId}`
    );
    if (existingSessionId) {
      await this.deleteSessionInternal(existingSessionId);
    }

    const expiryMinutes = parseInt(this.env.CODE_EXPIRY_MINUTES || "5", 10);
    const now = Date.now();

    const session: Session = {
      id: generateSessionId(),
      userId: body.userId,
      code: generateVerificationCode(),
      verified: false,
      expiresAt: now + expiryMinutes * 60 * 1000,
      createdAt: now,
      callbackUrl: body.callbackUrl,
    };

    await this.state.storage.put({
      [`session:${session.id}`]: session,
      [`session_by_user:${body.userId}`]: session.id,
    });

    await this.ensureAlarmScheduled();

    return Response.json(session);
  }

  private async getSession(sessionId: string): Promise<Response> {
    const session = await this.state.storage.get<Session>(
      `session:${sessionId}`
    );

    if (!session) {
      return new Response(JSON.stringify({ error: "Session not found" }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      });
    }

    if (Date.now() > session.expiresAt && !session.verified) {
      await this.deleteSessionInternal(sessionId, session);
      return new Response(JSON.stringify({ error: "Session expired" }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      });
    }

    return Response.json(session);
  }

  private async deleteSession(sessionId: string): Promise<Response> {
    await this.deleteSessionInternal(sessionId);
    return new Response(null, { status: 204 });
  }

  private async deleteSessionInternal(sessionId: string, existingSession?: Session): Promise<void> {
    const session = existingSession ??
      (await this.state.storage.get<Session>(`session:${sessionId}`));
    if (session) {
      await this.state.storage.delete([
        `session:${sessionId}`,
        `session_by_user:${session.userId}`,
        `friend:${session.userId}`,
      ]);
    }
  }

  private async startAlarm(): Promise<Response> {
    await this.ensureAlarmScheduled();
    return new Response("Alarm started", { status: 200 });
  }

  private async ensureAlarmScheduled(): Promise<void> {
    const currentAlarm = await this.state.storage.getAlarm();
    if (!currentAlarm) {
      await this.state.storage.setAlarm(Date.now() + 30000);
    }
  }

  private async checkRateLimit(userId: string): Promise<{ allowed: boolean; resetAt: number }> {
    const maxTokens = parseInt(this.env.RATE_LIMIT_PER_HOUR || "3", 10);
    const refillInterval = 60 * 60 * 1000;
    const now = Date.now();

    const stored = await this.state.storage.get<RateLimitState>(`ratelimit:${userId}`);
    const state: RateLimitState = stored ?? { tokens: maxTokens, lastRefill: now };

    if (stored) {
      const elapsed = now - state.lastRefill;
      const tokensToAdd = Math.floor(elapsed / refillInterval) * maxTokens;
      if (tokensToAdd > 0) {
        state.tokens = Math.min(maxTokens, state.tokens + tokensToAdd);
        state.lastRefill = now;
      }
    }

    const resetAt = state.lastRefill + refillInterval;
    if (state.tokens <= 0) {
      return { allowed: false, resetAt };
    }

    state.tokens--;
    await this.state.storage.put(`ratelimit:${userId}`, state);
    return { allowed: true, resetAt };
  }

  async alarm(): Promise<void> {
    const entries = await this.state.storage.list<Session>({ prefix: "session:" });
    const now = Date.now();
    const activeSessions: Session[] = [];
    const expiredSessions: Session[] = [];

    for (const [, session] of entries) {
      if (session.verified) continue;
      if (session.expiresAt > now) {
        activeSessions.push(session);
      } else {
        expiredSessions.push(session);
      }
    }

    await Promise.allSettled([
      this.processPendingFriendRequests(),
      this.monitorChatMessages(activeSessions),
      this.handleExpiredSessions(expiredSessions),
    ]);

    if (activeSessions.length > 0) {
      await this.state.storage.setAlarm(Date.now() + 30000);
    }
  }

  private async processPendingFriendRequests(): Promise<void> {
    const client = this.getClient();
    const pendingRequests = await client.getPendingFriendRequests();

    const userKeys = pendingRequests.map(r => `session_by_user:${r.userId}`);
    const userSessionMap = await this.state.storage.get<string>(userKeys);

    const sessionIds = [...userSessionMap.values()].filter(Boolean);
    const sessionKeys = sessionIds.map(id => `session:${id}`);
    const sessionsMap = await this.state.storage.get<Session>(sessionKeys);

    for (const request of pendingRequests) {
      const sessionId = userSessionMap.get(`session_by_user:${request.userId}`);
      if (!sessionId) continue;

      const session = sessionsMap.get(`session:${sessionId}`);
      if (!session || session.verified || Date.now() > session.expiresAt) continue;

      const accepted = await client.acceptFriendRequest(request.userId);
      if (accepted) {
        await this.state.storage.put(`friend:${request.userId}`, true);
        console.log(`[verification] Accepted friend request from ${request.userId}`);
      }
    }
  }

  private async monitorChatMessages(sessions: Session[]): Promise<void> {
    if (sessions.length === 0) return;
    const client = this.getClient();

    const uncachedUserIds: string[] = [];
    const friendStatus = new Map<string, boolean>();

    const friendKeys = sessions.map(s => `friend:${s.userId}`);
    const cachedFriends = await this.state.storage.get<boolean>(friendKeys);
    for (const session of sessions) {
      const cached = cachedFriends.get(`friend:${session.userId}`);
      if (cached !== undefined) {
        friendStatus.set(session.userId, cached);
      } else {
        uncachedUserIds.push(session.userId);
      }
    }

    if (uncachedUserIds.length > 0) {
      const allFriends = await client.getAllFriendIds();
      const friendEntries: Record<string, boolean> = {};
      for (const userId of uncachedUserIds) {
        const isFriend = allFriends.has(userId);
        friendStatus.set(userId, isFriend);
        friendEntries[`friend:${userId}`] = isFriend;
      }
      await this.state.storage.put(friendEntries);
    }

    // TODO: Could parallelize readChatMessages calls if GeoGuessr API rate limits allow it
    for (const session of sessions) {
      const isFriend = friendStatus.get(session.userId);
      if (!isFriend) continue;

      const messages = await client.readChatMessages(session.userId);

      if (messages === null) {
        await this.state.storage.delete(`friend:${session.userId}`);
        continue;
      }

      for (const msg of messages) {
        if (msg.textPayload && msg.textPayload.toUpperCase().includes(session.code)) {
          session.verified = true;
          await this.state.storage.put(`session:${session.id}`, session);
          this.sendSessionWebhook(session, "verified");
          await this.state.storage.delete(`friend:${session.userId}`);
          console.log(`[verification] Session ${session.id} verified for user ${session.userId}`);
          break;
        }
      }
    }
  }

  private sendSessionWebhook(session: Session, status: CallbackPayload["status"]): void {
    if (!session.callbackUrl) return;
    const payload: CallbackPayload = {
      session_id: session.id,
      user_id: session.userId,
      status,
      timestamp: new Date().toISOString(),
    };
    console.log(`[verification] Sending ${status} webhook for session ${session.id}`);
    this.state.waitUntil(sendWebhook(session.callbackUrl, payload, this.env.API_KEY));
  }

  private async handleExpiredSessions(expiredSessions: Session[]): Promise<void> {
    if (expiredSessions.length === 0) return;
    const keysToDelete: string[] = [];

    for (const session of expiredSessions) {
      this.sendSessionWebhook(session, "expired");
      keysToDelete.push(
        `session:${session.id}`,
        `session_by_user:${session.userId}`,
        `friend:${session.userId}`,
      );
      console.log(`[verification] Session ${session.id} expired for user ${session.userId}`);
    }

    await this.state.storage.delete(keysToDelete);
  }
}
