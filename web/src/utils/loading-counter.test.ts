import { describe, expect, it, vi } from "vitest";
import { createLoadingCounter } from "./loading-counter";

describe("createLoadingCounter", () => {
  it("stays active until every concurrent request finishes", () => {
    const onActiveChange = vi.fn();
    const counter = createLoadingCounter(onActiveChange);

    counter.begin();
    counter.begin();
    counter.finish();

    expect(counter.pending).toBe(1);
    expect(onActiveChange.mock.calls.map(call => call[0])).toEqual([true, true, true]);

    counter.finish();
    expect(counter.pending).toBe(0);
    expect(onActiveChange).toHaveBeenLastCalledWith(false);
  });

  it("does not underflow when cleanup runs twice", () => {
    const onActiveChange = vi.fn();
    const counter = createLoadingCounter(onActiveChange);

    counter.finish();
    counter.finish();

    expect(counter.pending).toBe(0);
    expect(onActiveChange).toHaveBeenLastCalledWith(false);
  });
});
