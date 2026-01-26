import type { RateLimitStatus } from "../types";

interface RateLimiterState {
  tokens: number;
  lastRefill: number;
}

export class UserRateLimiter implements DurableObject {
  private state: DurableObjectState;
  private maxTokens: number = 3;
  private refillInterval: number = 60 * 60 * 1000; // 1 hour in ms

  constructor(state: DurableObjectState) {
    this.state = state;
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const maxTokens = url.searchParams.get("maxTokens");
    if (maxTokens) {
      this.maxTokens = parseInt(maxTokens, 10);
    }

    if (request.method === "POST" && url.pathname === "/check") {
      return this.checkAndConsume();
    }

    if (request.method === "GET" && url.pathname === "/status") {
      return this.getStatus();
    }

    return new Response("Not Found", { status: 404 });
  }

  private async checkAndConsume(): Promise<Response> {
    const stored = await this.state.storage.get<RateLimiterState>("state");
    const now = Date.now();

    let state: RateLimiterState;
    if (!stored) {
      state = { tokens: this.maxTokens, lastRefill: now };
    } else {
      state = stored;
      const elapsed = now - state.lastRefill;
      const tokensToAdd = Math.floor(elapsed / this.refillInterval) * this.maxTokens;
      if (tokensToAdd > 0) {
        state.tokens = Math.min(this.maxTokens, state.tokens + tokensToAdd);
        state.lastRefill = now;
      }
    }

    if (state.tokens <= 0) {
      const resetAt = state.lastRefill + this.refillInterval;
      const result: RateLimitStatus = {
        allowed: false,
        remaining: 0,
        resetAt,
      };
      return Response.json(result);
    }

    state.tokens--;
    await this.state.storage.put("state", state);

    const resetAt = state.lastRefill + this.refillInterval;
    const result: RateLimitStatus = {
      allowed: true,
      remaining: state.tokens,
      resetAt,
    };
    return Response.json(result);
  }

  private async getStatus(): Promise<Response> {
    const stored = await this.state.storage.get<RateLimiterState>("state");
    const now = Date.now();

    if (!stored) {
      const result: RateLimitStatus = {
        allowed: true,
        remaining: this.maxTokens,
        resetAt: now + this.refillInterval,
      };
      return Response.json(result);
    }

    let state = stored;
    const elapsed = now - state.lastRefill;
    const tokensToAdd = Math.floor(elapsed / this.refillInterval) * this.maxTokens;
    if (tokensToAdd > 0) {
      state.tokens = Math.min(this.maxTokens, state.tokens + tokensToAdd);
    }

    const resetAt = state.lastRefill + this.refillInterval;
    const result: RateLimitStatus = {
      allowed: state.tokens > 0,
      remaining: state.tokens,
      resetAt,
    };
    return Response.json(result);
  }
}
