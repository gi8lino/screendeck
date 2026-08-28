import assert from "node:assert/strict";
import { after, before, beforeEach, test } from "node:test";
import { createRequire } from "node:module";

const require = createRequire(new URL("../../docs/package.json", import.meta.url));
const { chromium } = require("playwright");

const baseURL = process.env.E2E_BASE_URL || "http://127.0.0.1:18081";
let browser;

before(async () => {
  browser = await chromium.launch({ headless: true });
});

after(async () => {
  await browser?.close();
});

beforeEach(async () => {
  await apiFixture("/reset", { method: "POST" });
});

async function apiFixture(path, options = {}) {
  const response = await fetch(`${baseURL}/__e2e${path}`, options);
  assert.equal(response.ok, true, `fixture request failed: ${path}`);
  return response.json();
}

async function newPage() {
  const context = await browser.newContext();
  const page = await context.newPage();
  return { context, page };
}

async function configureFixture() {
  await apiFixture("/configure", { method: "POST" });
}

async function createRoom(page, name = "Host") {
  await page.goto(baseURL);
  await page.getByRole("button", { name: "Create a room" }).click();
  await page.getByLabel("Your name").fill(name);
  await page.getByRole("button", { name: "Create room" }).click();
  await page.getByRole("heading", { name: `Good hunting, ${name}.` }).waitFor();
}

async function joinRoom(page, name = "Guest") {
  await page.goto(`${baseURL}/?room=DECK42`);
  await page.getByRole("heading", { name: "Enter the room." }).waitFor();
  await page.getByLabel("Your name").fill(name);
  await page.getByRole("button", { name: "Join room" }).click();
  await page.getByRole("heading", { name: `Good hunting, ${name}.` }).waitFor();
}

async function likeCurrentTitle(page, expectMatch) {
  const title = await page.locator(".item-title").textContent();
  await page.getByTitle("Like").click();

  if (expectMatch) {
    const dialog = page
      .getByRole("dialog")
      .filter({ hasText: "It’s a match!" });
    await dialog.waitFor();
    await dialog.getByRole("button", { name: "Continue swiping" }).click();
  }

  await page.waitForFunction(
    (previousTitle) => {
      const current = document.querySelector(".item-title")?.textContent;
      return current !== previousTitle;
    },
    title,
  );
}

async function waitForFixture(predicate, timeout = 9000) {
  const started = Date.now();
  while (Date.now() - started < timeout) {
    const current = await apiFixture("/status");
    if (predicate(current)) return current;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  assert.fail("fixture condition was not reached before timeout");
}

test("connects Jellyfin through the setup flow", async () => {
  const { context, page } = await newPage();
  try {
    await page.goto(baseURL);
    await page.getByRole("button", { name: "Connect media server" }).click();
    await page.getByRole("button", { name: /Jellyfin/ }).click();
    await page.getByLabel("Server URL").fill("http://jellyfin.test:8096");
    await page.getByLabel("Username").fill("tester");
    await page.getByLabel("Password").fill("secret");
    await page.getByRole("button", { name: "Connect Jellyfin" }).click();
    await page.getByRole("button", { name: "Create a room" }).waitFor();
    assert.match(await page.locator("body").innerText(), /Jellyfin connected/);
  } finally {
    await context.close();
  }
});

test("creates a room and joins it from another browser", async () => {
  await configureFixture();
  const host = await newPage();
  const guest = await newPage();
  try {
    await createRoom(host.page);
    await joinRoom(guest.page);
    await host.page.getByText(/Guest/).waitFor();
    assert.match(await host.page.locator(".people").innerText(), /Host.*Guest/s);
  } finally {
    await host.context.close();
    await guest.context.close();
  }
});

test("votes on matches and advances unanimously to the next round", async () => {
  await configureFixture();
  const host = await newPage();
  const guest = await newPage();
  try {
    await createRoom(host.page);
    await joinRoom(guest.page);
    await host.page.getByText(/Guest/).waitFor();

    for (let match = 0; match < 2; match += 1) {
      await likeCurrentTitle(host.page, false);
      await likeCurrentTitle(guest.page, true);
    }

    await host.page.getByRole("button", { name: "Ask for next round" }).waitFor();
    await host.page.getByRole("button", { name: "Ask for next round" }).click();
    await guest.page
      .getByRole("button", { name: "Ready for next round" })
      .waitFor();
    await guest.page.getByRole("button", { name: "Ready for next round" }).click();

    await host.page.getByText(/Round 2/).first().waitFor();
    await guest.page.getByText(/Round 2/).first().waitFor();
    const fixture = await apiFixture("/status");
    assert.equal(fixture.round, 2);
  } finally {
    await host.context.close();
    await guest.context.close();
  }
});

test("reconnects the room event stream after interruption", async () => {
  await configureFixture();
  const { context, page } = await newPage();
  try {
    await createRoom(page);
    const before = await waitForFixture((current) => current.activeEvents === 1);
    await apiFixture("/drop-events", { method: "POST" });
    const after = await waitForFixture(
      (current) => current.eventConnections > before.eventConnections,
    );
    assert.ok(after.activeEvents >= 1);
    await page.getByRole("heading", { name: "Good hunting, Host." }).waitFor();
  } finally {
    await context.close();
  }
});
