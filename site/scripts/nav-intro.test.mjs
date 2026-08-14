import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const navSource = await readFile(new URL("../src/components/Nav.tsx", import.meta.url), "utf8");

test("homepage navigation appears when the brand intro completes", () => {
  assert.match(navSource, /setVisible\(!intro \|\| document\.documentElement\.dataset\.introComplete === "true"\)/);
  assert.match(navSource, /addEventListener\(INTRO_COMPLETION_EVENT, onIntroCompletion/);
});

test("navigation enters with a transition instead of delayed layout insertion", () => {
  assert.doesNotMatch(navSource, /!visible && "hidden"/);
  assert.match(navSource, /-translate-y-full opacity-0/);
  assert.match(navSource, /translate-y-0 opacity-100/);
});
