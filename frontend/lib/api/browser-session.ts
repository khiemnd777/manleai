export type LegacyTokenStorage = Pick<Storage, "removeItem">;

const legacyTokenKeys = ["access_token", "refresh_token"] as const;

export function createBrowserSession(
  resolveStorage: () => LegacyTokenStorage | undefined = defaultStorage
) {
  let accessToken = "";
  let legacyStorageCleared = false;

  function clearLegacyStorage() {
    if (legacyStorageCleared) return;
    legacyStorageCleared = true;
    try {
      const storage = resolveStorage();
      for (const key of legacyTokenKeys) {
        storage?.removeItem(key);
      }
    } catch {
      // Storage may be unavailable under a locked-down browser policy. The
      // active session still remains memory-only.
    }
  }

  return {
    getAccessToken() {
      clearLegacyStorage();
      return accessToken;
    },
    setAccessToken(value: string) {
      clearLegacyStorage();
      accessToken = value;
    },
    clear() {
      clearLegacyStorage();
      accessToken = "";
    }
  };
}

function defaultStorage(): LegacyTokenStorage | undefined {
  if (typeof window === "undefined") return undefined;
  return window.localStorage;
}

export const browserSession = createBrowserSession();

