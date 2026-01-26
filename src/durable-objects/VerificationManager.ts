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

    if (request.method === "POST" && url.pathname === "/session") {
      return this.createSession(request);
    }

    if (request.method === "GET" && url.pathname.startsWith("/session/")) {
      const sessionId = url.pathname.split("/")[2];
      return this.getSession(sessionId);
    }

    if (request.method === "DELETE" && url.pathname.startsWith("/session/")) {
      const sessionId = url.pathname.split("/")[2];
      return this.deleteSession(sessionId);
    }

    if (request.method === "POST" && url.pathname === "/start-alarm") {
      return this.startAlarm();
    }

    return new Response("Not Found", { status: 404 });
  }

  private async createSession(request: Request): Promise<Response> {
    const body = (await request.json()) as {
      userId: string;
      callbackUrl?: string;
    };

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

    await this.state.storage.put(`session:${session.id}`, session);
    await this.state.storage.put(`session_by_user:${body.userId}`, session.id);

    const isFriend = await this.getClient().isFriend(body.userId);
    if (isFriend) {
      await this.state.storage.put(`friend:${body.userId}`, true);
    }

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
      await this.deleteSessionInternal(sessionId);
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

  private async deleteSessionInternal(sessionId: string): Promise<void> {
    const session = await this.state.storage.get<Session>(
      `session:${sessionId}`
    );
    if (session) {
      await this.state.storage.delete(`session:${sessionId}`);
      await this.state.storage.delete(`session_by_user:${session.userId}`);
      await this.state.storage.delete(`friend:${session.userId}`);
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

  async alarm(): Promise<void> {
    try {
      await this.processPendingFriendRequests();
      await this.monitorChatMessages();
      await this.handleExpiredSessions();
    } catch (error) {
      console.error("[verification] Error in alarm handler:", error);
    }

    const activeSessions = await this.getActiveSessions();
    if (activeSessions.length > 0) {
      await this.state.storage.setAlarm(Date.now() + 30000);
    }
  }

  private async getActiveSessions(): Promise<Session[]> {
    const entries = await this.state.storage.list<Session>({
      prefix: "session:",
    });
    const sessions: Session[] = [];
    const now = Date.now();

    for (const [, session] of entries) {
      if (!session.verified && session.expiresAt > now) {
        sessions.push(session);
      }
    }

    return sessions;
  }

  private async processPendingFriendRequests(): Promise<void> {
    const client = this.getClient();
    const pendingRequests = await client.getPendingFriendRequests();

    for (const request of pendingRequests) {
      const sessionId = await this.state.storage.get<string>(
        `session_by_user:${request.userId}`
      );
      if (!sessionId) continue;

      const session = await this.state.storage.get<Session>(
        `session:${sessionId}`
      );
      if (!session || session.verified || Date.now() > session.expiresAt) continue;

      const accepted = await client.acceptFriendRequest(request.userId);
      if (accepted) {
        await this.state.storage.put(`friend:${request.userId}`, true);
        console.log(`[verification] Accepted friend request from ${request.userId}`);
      }
    }
  }

  private async monitorChatMessages(): Promise<void> {
    const client = this.getClient();
    const sessions = await this.getActiveSessions();

    for (const session of sessions) {
      const isFriend = await this.state.storage.get<boolean>(
        `friend:${session.userId}`
      );
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

          if (session.callbackUrl) {
            const payload: CallbackPayload = {
              session_id: session.id,
              user_id: session.userId,
              status: "verified",
              timestamp: new Date().toISOString(),
            };
            await sendWebhook(session.callbackUrl, payload);
          }

          await this.state.storage.delete(`friend:${session.userId}`);
          console.log(`[verification] Session ${session.id} verified for user ${session.userId}`);
          break;
        }
      }
    }
  }

  private async handleExpiredSessions(): Promise<void> {
    const entries = await this.state.storage.list<Session>({
      prefix: "session:",
    });
    const now = Date.now();

    for (const [key, session] of entries) {
      if (!session.verified && session.expiresAt <= now) {
        if (session.callbackUrl) {
          const payload: CallbackPayload = {
            session_id: session.id,
            user_id: session.userId,
            status: "expired",
            timestamp: new Date().toISOString(),
          };
          await sendWebhook(session.callbackUrl, payload);
        }

        await this.state.storage.delete(key);
        await this.state.storage.delete(`session_by_user:${session.userId}`);
        await this.state.storage.delete(`friend:${session.userId}`);
        console.log(`[verification] Session ${session.id} expired for user ${session.userId}`);
      }
    }
  }
}
