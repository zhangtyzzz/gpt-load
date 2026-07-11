// @ts-expect-error Vitest executes this contract test in Node; app types intentionally stay browser-only.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import globalProvidersSource from "./GlobalProviders.vue?raw";

const globalStyleSource = readFileSync("src/assets/style.css", "utf8");

describe("GlobalProviders message safety", () => {
  it("gives teleported messages a global positioning hook", () => {
    expect(globalProvidersSource).toMatch(
      /<n-message-provider\s+placement="top-right"\s+container-class="global-message-container">/
    );
  });

  it("keeps mobile messages below header actions and within safe-area edges", () => {
    expect(globalStyleSource).toMatch(
      /body \.global-message-container\.n-message-container--top-right\s*\{[\s\S]*?top:\s*max\(var\(--space-3\), env\(safe-area-inset-top, 0px\)\)/
    );
    expect(globalStyleSource).toMatch(
      /@media \(max-width: 768px\)[\s\S]*?body \.global-message-container\.n-message-container--top-right\s*\{[\s\S]*?top:\s*calc\(max\(var\(--space-3\), env\(safe-area-inset-top, 0px\)\) \+ 4rem\)[\s\S]*?right:\s*max\(var\(--space-3\), env\(safe-area-inset-right, 0px\)\)[\s\S]*?left:\s*max\(var\(--space-3\), env\(safe-area-inset-left, 0px\)\)[\s\S]*?align-items:\s*center/
    );
    expect(globalStyleSource).toMatch(
      /\.global-message-container \.n-message\s*\{\s*max-width:\s*100%/
    );
  });
});
