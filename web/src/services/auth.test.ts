import { beforeEach, describe, expect, it } from "vitest";
import { useAuthService } from "./auth";

describe("useAuthService logout transaction", () => {
  beforeEach(() => {
    useAuthService().logout();
    localStorage.clear();
  });

  it("keeps the credential while guarded navigation is pending or cancelled", () => {
    localStorage.setItem("authKey", "test-console-key");
    const auth = useAuthService();

    expect(auth.checkLogin()).toBe(true);
    auth.beginLogout();
    expect(auth.checkLogin()).toBe(false);
    expect(localStorage.getItem("authKey")).toBe("test-console-key");

    auth.cancelLogout();
    expect(auth.checkLogin()).toBe(true);
    expect(localStorage.getItem("authKey")).toBe("test-console-key");
  });

  it("removes the credential only after logout commits", () => {
    localStorage.setItem("authKey", "test-console-key");
    const auth = useAuthService();

    expect(auth.checkLogin()).toBe(true);
    auth.beginLogout();
    auth.logout();

    expect(localStorage.getItem("authKey")).toBeNull();
    expect(auth.checkLogin()).toBe(false);
  });
});
