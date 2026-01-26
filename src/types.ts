export interface Env {
  VERIFICATION_MANAGER: DurableObjectNamespace;
  USER_RATE_LIMITER: DurableObjectNamespace;
  GEOGUESSR_NCFA_TOKEN: string;
  API_KEY: string;
  CODE_EXPIRY_MINUTES: string;
  RATE_LIMIT_PER_HOUR: string;
  ALLOWED_CALLBACK_HOSTS: string;
}

export interface Session {
  id: string;
  userId: string;
  code: string;
  verified: boolean;
  expiresAt: number;
  createdAt: number;
  callbackUrl?: string;
}

export interface Friend {
  userId: string;
  nick: string;
  countryCode: string;
  isFriend: boolean;
  isBlocked: boolean;
  isBlockedBy: boolean;
  flair: number;
  pin: {
    url: string;
    anchor: string;
    isDefault: boolean;
  };
}

export interface PendingFriendRequest {
  userId: string;
  nick: string;
  created: string;
  pin?: {
    url: string;
    anchor: string;
    isDefault: boolean;
  };
}

export interface ChatMessage {
  id: string;
  payloadType: string;
  textPayload: string;
  sourceId: string;
  sourceType: string;
  recipientId: string;
  sentAt: string;
  roomId: string;
}

export interface ChatResponse {
  messages: ChatMessage[];
}

export interface StartVerificationRequest {
  user_id: string;
  callback_url?: string;
}

export interface StartVerificationResponse {
  session_id: string;
  verification_code: string;
  expires_at: string;
}

export interface SessionStatusResponse {
  session_id: string;
  user_id: string;
  verified: boolean;
  expires_at: string;
  created_at: string;
}

export interface CallbackPayload {
  session_id: string;
  user_id: string;
  status: "verified" | "expired";
  timestamp: string;
}

export interface ErrorResponse {
  error: string;
}

export interface RateLimitStatus {
  allowed: boolean;
  remaining: number;
  resetAt: number;
}
