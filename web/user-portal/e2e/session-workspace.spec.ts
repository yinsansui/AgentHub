import { test, expect } from "@playwright/test";

test.describe("Session 列表和 Workspace 切换", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/?workspaceId=ws_test_e2e");
    await page.waitForSelector("text=新 Session", { timeout: 10000 });
  });

  test("应该显示 workspace 切换下拉菜单", async ({ page }) => {
    await page.click("button:has-text('ws_test_e2e')");
    await page.waitForTimeout(500);
    await expect(page.locator("input[placeholder='输入 workspace ID']")).toBeVisible();
    await page.keyboard.press("Escape");
  });

  test("应该能切换到另一个 workspace", async ({ page }) => {
    await page.click("button:has-text('ws_test_e2e')");
    await page.fill("input[placeholder='输入 workspace ID']", "ws_test_e2e_2");
    await page.click("button[aria-label='切换 Workspace']");
    await page.waitForURL(/workspaceId=ws_test_e2e_2/);
    await expect(page.locator("button:has-text('ws_test_e2e_2')")).toBeVisible();
  });

  test("session 列表应该能加载", async ({ page }) => {
    await page.waitForTimeout(2000);
    const sessionList = page.locator("div.flex-1.overflow-y-auto");
    await expect(sessionList).toBeVisible();
  });

  test("新建 session 后应该出现在列表中", async ({ page }) => {
    await page.click("button:has-text('新 Session')");
    await page.waitForSelector("textarea[placeholder='给 AgentHub 发送消息…']", { timeout: 5000 });
    await page.fill("textarea[placeholder='给 AgentHub 发送消息…']", "Hello, this is a test message");
    await page.click("button[aria-label='发送']");
    await page.waitForTimeout(2000);
    const errorToast = page.locator("div.notice-toast.error");
    const hasError = await errorToast.isVisible().catch(() => false);
    if (hasError) {
      test.skip(true, "后端未配置 LLM，跳过此测试");
      return;
    }
    await page.waitForTimeout(5000);
    await page.reload();
    await page.waitForTimeout(3000);
    const sessionItems = page.locator("div.flex-1.overflow-y-auto button");
    const count = await sessionItems.count();
    expect(count).toBeGreaterThan(0);
  });

  test("workspace 切换后 session 列表应该隔离", async ({ page }) => {
    await page.click("button:has-text('新 Session')");
    await page.waitForSelector("textarea[placeholder='给 AgentHub 发送消息…']", { timeout: 5000 });
    await page.fill("textarea[placeholder='给 AgentHub 发送消息…']", "Test in workspace 1");
    await page.click("button[aria-label='发送']");
    await page.waitForTimeout(2000);
    const errorToast = page.locator("div.notice-toast.error");
    const hasError = await errorToast.isVisible().catch(() => false);
    if (hasError) {
      test.skip(true, "后端未配置 LLM，跳过此测试");
      return;
    }
    await page.waitForTimeout(5000);
    await page.reload();
    await page.waitForTimeout(3000);
    const sessionItems1 = page.locator("div.flex-1.overflow-y-auto button");
    const count1 = await sessionItems1.count();
    await page.click("button:has-text('ws_test_e2e')");
    await page.fill("input[placeholder='输入 workspace ID']", "ws_test_e2e_3");
    await page.click("button[aria-label='切换 Workspace']");
    await page.waitForURL(/workspaceId=ws_test_e2e_3/);
    await page.waitForTimeout(3000);
    const sessionItems2 = page.locator("div.flex-1.overflow-y-auto button");
    const count2 = await sessionItems2.count();
    expect(count1).not.toEqual(count2);
  });
});
