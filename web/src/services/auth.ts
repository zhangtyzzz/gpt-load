import http from "@/utils/http";
import { useState } from "@/utils/state";

const AUTH_KEY = "authKey";
let logoutPending = false;

export const useAuthKey = () => {
  return useState<string | null>(AUTH_KEY, () => null);
};

export function useAuthService() {
  const authKey = useAuthKey();

  const login = async (key: string): Promise<boolean> => {
    try {
      await http.post("/auth/login", { auth_key: key });
      localStorage.setItem(AUTH_KEY, key);
      authKey.value = key;
      return true;
    } catch (_error) {
      // 错误已记录
      return false;
    }
  };

  const logout = (): void => {
    logoutPending = false;
    localStorage.removeItem(AUTH_KEY);
    authKey.value = null;
  };

  // Let the router evaluate page-level leave guards before destroying the
  // current session. A cancelled dirty-form navigation can then keep the user
  // authenticated without copying the credential into the page component.
  const beginLogout = (): void => {
    logoutPending = true;
  };

  const cancelLogout = (): void => {
    logoutPending = false;
  };

  const checkLogin = (): boolean => {
    if (logoutPending) {
      return false;
    }
    if (authKey.value) {
      return true;
    }

    const key = localStorage.getItem(AUTH_KEY);
    if (key) {
      authKey.value = key;
    }
    return !!authKey.value;
  };

  return {
    login,
    logout,
    beginLogout,
    cancelLogout,
    checkLogin,
  };
}
