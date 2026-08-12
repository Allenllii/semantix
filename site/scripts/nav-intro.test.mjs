import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const navSource = await readFile(new URL("../src/components/Nav.tsx", import.meta.url), "utf8");

test("homepage navigation reacts to the first downward scroll intent", () => {
  assert.match(navSource, /event\.deltaY > 0/);
  assert.match(navSource, /addEventListener\("wheel", onScrollIntent/);
  assert.match(navSource, /addEventListener\("touchmove", onTouchMove/);
  assert.match(navSource, /setVisible\(!intro \|\| window\.scrollY > 8\)/);
});

test("navigation enters with a transition instead of delayed layout insertion", () => {
  assert.doesNotMatch(navSource, /!visible && "hidden"/);
  assert.match(navSource, /-translate-y-full opacity-0/);
  assert.match(navSource, /translate-y-0 opacity-100/);
});
