import type {
  Friend,
  PendingFriendRequest,
  ChatMessage,
  ChatResponse,
} from "../types";

const BASE_URL = "https://www.geoguessr.com/api/v3";
const BASE_URL_V4 = "https://www.geoguessr.com/api/v4";

export class GeoGuessrClient {
  private ncfaToken: string;

  constructor(ncfaToken: string) {
    this.ncfaToken = ncfaToken;
  }

  private getHeaders(): HeadersInit {
    return {
      Cookie: `_ncfa=${this.ncfaToken}`,
      "Content-Type": "application/json",
      Origin: "https://www.geoguessr.com",
      Referer: "https://www.geoguessr.com/",
    };
  }

  async isFriend(userId: string): Promise<boolean> {
    const pageSize = 50;
    let page = 0;

    while (true) {
      const url = `${BASE_URL}/social/friends?count=${pageSize}&page=${page}`;
      const response = await fetch(url, {
        headers: this.getHeaders(),
        signal: AbortSignal.timeout(30000),
      });

      if (!response.ok) {
        console.error(`[geoguessr] Failed to fetch friends: ${response.status}`);
        return false;
      }

      const friends = (await response.json()) as Friend[];

      if (friends.some((f) => f.userId === userId)) {
        return true;
      }

      if (friends.length < pageSize) {
        break;
      }

      page++;
      await this.delay(1000);
    }

    return false;
  }

  async getPendingFriendRequests(): Promise<PendingFriendRequest[]> {
    const url = `${BASE_URL}/social/friends/received`;
    const response = await fetch(url, {
      headers: this.getHeaders(),
      signal: AbortSignal.timeout(30000),
    });

    if (!response.ok) {
      console.error(`[geoguessr] Failed to fetch pending friend requests: ${response.status}`);
      return [];
    }

    return (await response.json()) as PendingFriendRequest[] || [];
  }

  async acceptFriendRequest(userId: string): Promise<boolean> {
    const url = `${BASE_URL}/social/friends/${userId}?context=`;
    const response = await fetch(url, {
      method: "PUT",
      headers: this.getHeaders(),
      signal: AbortSignal.timeout(30000),
    });

    if (!response.ok && response.status !== 201) {
      console.error(`[geoguessr] Failed to accept friend request from ${userId}: ${response.status}`);
      return false;
    }

    return true;
  }

  async readChatMessages(userId: string): Promise<ChatMessage[] | null> {
    const url = `${BASE_URL_V4}/chat/${userId}`;
    const response = await fetch(url, {
      headers: this.getHeaders(),
      signal: AbortSignal.timeout(30000),
    });

    if (response.status === 404) {
      return null;
    }

    if (!response.ok) {
      console.error(`[geoguessr] Failed to read chat messages for ${userId}: ${response.status}`);
      return [];
    }

    const data = (await response.json()) as ChatResponse;
    return data.messages || [];
  }

  async isLoggedIn(): Promise<boolean> {
    const url = `${BASE_URL}/accounts/me`;
    const response = await fetch(url, {
      headers: this.getHeaders(),
      signal: AbortSignal.timeout(30000),
    });

    return response.ok;
  }

  private delay(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}
