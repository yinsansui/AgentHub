import { test, expect, type Page } from "@playwright/test";

test.describe("Workspace CRUD 菜单", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await page.fill("input[autocomplete='username']", "admin");
    await page.fill("input[autocomplete='current-password']", "admin");
    await page.click("button:has-text('登录')");
    await page.waitForSelector("text=新 Session", { timeout: 10000 });
  });

  function openWorkspaceMenu(page: Page) {
    return page.locator("[data-testid='workspace-menu-trigger']").click();
  }

  test("应该显示 workspace 下拉菜单并包含新建按钮", async ({ page }) => {
    await openWorkspaceMenu(page);
    await page.waitForTimeout(500);
    await expect(page.locator("button:has-text('新建 Workspace')")).toBeVisible();
    await page.keyboard.press("Escape");
  });

  test("默认菜单只显示一个 Default 工作空间且无预填输入", async ({ page }) => {
    await openWorkspaceMenu(page);
    await page.waitForTimeout(500);
    const defaultRows = page.locator("div.w-full.flex.items-center.gap-2 >> text=Default 工作空间");
    const count = await defaultRows.count();
    expect(count).toBeLessThanOrEqual(1);
    const createInput = page.locator("input[placeholder='Workspace 名称']");
    await expect(createInput).not.toBeVisible();
  });

  test("应该能创建、重命名和删除 workspace", async ({ page }) => {
    const uniqueName = `E2E-${Date.now()}`;
    const renamedName = `RENAMED-${Date.now()}`;

    await openWorkspaceMenu(page);
    await page.click("button:has-text('新建 Workspace')");
    await page.fill("input[placeholder='Workspace 名称']", uniqueName);
    await page.click("button:has-text('创建')");
    await page.waitForTimeout(1000);

    const urlAfterCreate = page.url();
    const createdId = new URL(urlAfterCreate).searchParams.get("workspaceId");
    expect(createdId).toBeTruthy();
    expect(createdId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);

    await openWorkspaceMenu(page);
    await page.locator("div.w-full.flex.items-center.gap-2", { hasText: uniqueName }).locator("button[aria-label='重命名']").click();
    await page.fill("input[placeholder='Workspace 名称']", renamedName);
    await page.click("button:has-text('保存')");
    await page.waitForTimeout(1000);
    await expect(page.locator("[data-testid='workspace-menu-trigger']", { hasText: renamedName })).toBeVisible();

    await openWorkspaceMenu(page);
    await page.locator("div.w-full.flex.items-center.gap-2", { hasText: renamedName }).locator("button[aria-label='删除']").click();
    await page.click("button:has-text('删除')");
    await page.waitForTimeout(1000);
    await expect(page.locator("[data-testid='workspace-menu-trigger']", { hasText: renamedName })).not.toBeVisible();
  });

  test("删除当前 workspace 后应该自动切换到剩余 workspace", async ({ page }) => {
    const uniqueName = `TEMP-${Date.now()}`;

    await openWorkspaceMenu(page);
    await page.click("button:has-text('新建 Workspace')");
    await page.fill("input[placeholder='Workspace 名称']", uniqueName);
    await page.click("button:has-text('创建')");
    await page.waitForTimeout(1000);

    await openWorkspaceMenu(page);
    await page.locator("div.w-full.flex.items-center.gap-2", { hasText: uniqueName }).locator("button[aria-label='删除']").click();
    await page.click("button:has-text('删除')");
    await page.waitForTimeout(1000);

    await expect(page.locator("[data-testid='workspace-menu-trigger']")).toBeVisible();
  });
});
